package vid

import (
	"errors"
	"strconv"
	"strings"
)

type CAVLCResidualBlock struct {
	Coefficients []int32
	TotalCoeff   uint8
}

type cavlcNode struct {
	next  [2]int
	value int
}

type cavlcVLC []cavlcNode

func newCAVLCVLC() cavlcVLC { return cavlcVLC{{value: -1}} }

func (table *cavlcVLC) insert(code string, value int) {
	index := 0
	for _, bit := range code {
		branch := int(bit - '0')
		if (*table)[index].next[branch] == 0 {
			next := len(*table)
			*table = append(*table, cavlcNode{value: -1})
			(*table)[index].next[branch] = next
		}
		index = (*table)[index].next[branch]
	}
	if (*table)[index].value != -1 {
		panic("duplicate CAVLC codeword")
	}
	(*table)[index].value = value
}

func (table cavlcVLC) read(reader *BitReader) (int, error) {
	index := 0
	for table[index].value < 0 {
		bit, err := reader.ReadBits(1)
		if err != nil {
			return 0, err
		}
		index = table[index].next[bit]
		if index == 0 {
			return 0, errors.New("invalid CAVLC codeword")
		}
	}
	return table[index].value, nil
}

type cavlcTables struct {
	tokens  [5]cavlcVLC
	zeros   []cavlcVLC
	chroma4 []cavlcVLC
	chroma8 []cavlcVLC
	runs    []cavlcVLC
}

func makeCAVLCTables() cavlcTables {
	var tables cavlcTables
	for index := range tables.tokens {
		tables.tokens[index] = newCAVLCVLC()
	}
	for _, row := range strings.Split(cavlcCoeffToken, "\n") {
		fields := strings.Fields(row)
		if len(fields) == 0 {
			continue
		}
		total, err := strconv.Atoi(fields[0])
		if err != nil {
			panic(err)
		}
		trailing, err := strconv.Atoi(fields[1])
		if err != nil {
			panic(err)
		}
		for column, code := range fields[2:] {
			if code != "-" {
				tables.tokens[column].insert(code, total*4+trailing)
			}
		}
	}
	makeRows := func(rows []string) []cavlcVLC {
		result := make([]cavlcVLC, len(rows))
		for index, row := range rows {
			result[index] = newCAVLCVLC()
			for value, code := range strings.Fields(row) {
				result[index].insert(code, value)
			}
		}
		return result
	}
	tables.zeros = makeRows(cavlcTotalZeros)
	tables.chroma4 = makeRows(cavlcChromaZeros4)
	tables.chroma8 = makeRows(cavlcChromaZeros8)
	tables.runs = makeRows(cavlcRunBefore)
	return tables
}

var cavlcLookup = makeCAVLCTables()

func DecodeCAVLCResidualBlock(reader *BitReader, nc, maxCoeff int) (CAVLCResidualBlock, error) {
	valid := nc == -1 && maxCoeff == 4 || nc == -2 && maxCoeff == 8 || nc >= 0 && nc <= 16 && (maxCoeff == 15 || maxCoeff == 16)
	if reader == nil || !valid {
		return CAVLCResidualBlock{}, errors.New("invalid CAVLC block context or coefficient count")
	}
	trial := *reader
	block, err := decodeCAVLCBlock(&trial, nc, maxCoeff)
	if err != nil {
		return CAVLCResidualBlock{}, err
	}
	*reader = trial
	return block, nil
}

func decodeCAVLCBlock(reader *BitReader, nc, maxCoeff int) (CAVLCResidualBlock, error) {
	var token int
	if nc >= 8 {
		bits, err := reader.ReadBits(6)
		if err != nil {
			return CAVLCResidualBlock{}, err
		}
		if bits != 3 {
			token = int((bits/4+1)*4 + bits%4)
		}
	} else {
		column := 0
		switch {
		case nc == -2:
			column = 4
		case nc == -1:
			column = 3
		case nc >= 4:
			column = 2
		case nc >= 2:
			column = 1
		}
		var err error
		token, err = cavlcLookup.tokens[column].read(reader)
		if err != nil {
			return CAVLCResidualBlock{}, err
		}
	}
	total, trailing := token/4, token%4
	if total > maxCoeff || trailing > total {
		return CAVLCResidualBlock{}, errors.New("CAVLC coeff_token exceeds block capacity")
	}
	block := CAVLCResidualBlock{Coefficients: make([]int32, maxCoeff), TotalCoeff: uint8(total)}
	if total == 0 {
		return block, nil
	}
	levels := make([]int64, total)
	for index := 0; index < trailing; index++ {
		bit, err := reader.ReadBit()
		if err != nil {
			return CAVLCResidualBlock{}, err
		}
		levels[index] = 1
		if bit {
			levels[index] = -1
		}
	}
	suffixLength := 0
	if total > 10 && trailing < 3 {
		suffixLength = 1
	}
	for index := trailing; index < total; index++ {
		prefix := 0
		for {
			bit, err := reader.ReadBit()
			if err != nil {
				return CAVLCResidualBlock{}, err
			}
			if bit {
				break
			}
			prefix++
			if prefix > 31 {
				return CAVLCResidualBlock{}, errors.New("CAVLC level prefix exceeds supported range")
			}
		}
		suffixSize := suffixLength
		if prefix == 14 && suffixLength == 0 {
			suffixSize = 4
		} else if prefix >= 15 {
			suffixSize = prefix - 3
		}
		suffix, err := reader.ReadBits(suffixSize)
		if err != nil {
			return CAVLCResidualBlock{}, err
		}
		code := int64(min(prefix, 15))<<suffixLength + int64(suffix)
		if prefix >= 15 && suffixLength == 0 {
			code += 15
		}
		if prefix >= 16 {
			code += (int64(1) << (prefix - 3)) - 4096
		}
		if index == trailing && trailing < 3 {
			code += 2
		}
		level := (code + 2) / 2
		if code&1 != 0 {
			level = -(code + 1) / 2
		}
		if level < -1<<31 || level > 1<<31-1 {
			return CAVLCResidualBlock{}, errors.New("CAVLC coefficient exceeds int32")
		}
		levels[index] = level
		suffixLength = max(suffixLength, 1)
		magnitude := level
		if magnitude < 0 {
			magnitude = -magnitude
		}
		if suffixLength < 6 && magnitude > int64(3)<<(suffixLength-1) {
			suffixLength++
		}
	}
	zerosLeft := 0
	if total < maxCoeff {
		zeros := cavlcLookup.zeros
		if maxCoeff == 4 {
			zeros = cavlcLookup.chroma4
		} else if maxCoeff == 8 {
			zeros = cavlcLookup.chroma8
		}
		var err error
		zerosLeft, err = zeros[total-1].read(reader)
		if err != nil {
			return CAVLCResidualBlock{}, err
		}
	}
	if zerosLeft > maxCoeff-total {
		return CAVLCResidualBlock{}, errors.New("CAVLC total_zeros exceeds block capacity")
	}
	runs := make([]int, total)
	for index := 0; index < total-1; index++ {
		if zerosLeft != 0 {
			var err error
			runs[index], err = cavlcLookup.runs[min(zerosLeft, 7)-1].read(reader)
			if err != nil {
				return CAVLCResidualBlock{}, err
			}
			if runs[index] > zerosLeft {
				return CAVLCResidualBlock{}, errors.New("CAVLC run_before exceeds zeros left")
			}
			zerosLeft -= runs[index]
		}
	}
	runs[total-1] = zerosLeft
	position := 0
	for index := total - 1; index >= 0; index-- {
		position += runs[index]
		block.Coefficients[position] = int32(levels[index])
		position++
	}
	return block, nil
}
