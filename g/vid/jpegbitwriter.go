package vid

import "errors"

var ErrJPEGEncoderInvalidBitCount = errors.New("JPEG codeword bit count must be between 1 and 16")
var ErrJPEGEncoderBitWriterFinished = errors.New("JPEG bit writer is already finished")

type jpegBitWriter struct {
	data     []byte
	current  byte
	bitCount uint8
	finished bool
}

func (writer *jpegBitWriter) WriteBits(value uint16, bitCount uint8) error {
	if writer.finished {
		return ErrJPEGEncoderBitWriterFinished
	}
	if bitCount == 0 || bitCount > 16 || value>>bitCount != 0 {
		return ErrJPEGEncoderInvalidBitCount
	}
	for bit := int(bitCount) - 1; bit >= 0; bit-- {
		writer.current = writer.current<<1 | byte((value>>bit)&1)
		writer.bitCount++
		if writer.bitCount == 8 {
			writer.flushByte()
		}
	}
	return nil
}

func (writer *jpegBitWriter) WriteCodewords(codewords []jpegCodeword) error {
	for _, codeword := range codewords {
		if err := writer.WriteBits(codeword.value, codeword.bitCount); err != nil {
			return err
		}
	}
	return nil
}

func (writer *jpegBitWriter) Finish() []byte {
	if writer.finished {
		return writer.data
	}
	if writer.bitCount > 0 {
		for writer.bitCount < 8 {
			writer.current = writer.current<<1 | 1
			writer.bitCount++
		}
		writer.flushByte()
	}
	writer.finished = true
	return writer.data
}

func (writer *jpegBitWriter) flushByte() {
	writer.data = append(writer.data, writer.current)
	if writer.current == 0xff {
		writer.data = append(writer.data, 0)
	}
	writer.current = 0
	writer.bitCount = 0
}
