package vid

func jpegEncodeScan(image jpegYCbCr444, lumaQuantization, chromaQuantization [64]uint8, specs []jpegHuffmanSpec) ([]byte, error) {
	var dcTables, acTables [4]jpegHuffmanTable
	for _, spec := range specs {
		table, err := newJPEGHuffmanTable(spec.counts, spec.symbols)
		if err != nil {
			return nil, err
		}
		if spec.class == 0 {
			dcTables[spec.id] = table
		} else if spec.class == 1 {
			acTables[spec.id] = table
		} else {
			return nil, ErrJPEGEncoderInvalidHuffmanTable
		}
	}

	planes := [3][]uint8{image.y, image.cb, image.cr}
	quantization := [3][64]uint8{lumaQuantization, chromaQuantization, chromaQuantization}
	previousDC := [3]int16{}
	blockColumns := (image.width + 7) / 8
	blockRows := (image.height + 7) / 8
	var writer jpegBitWriter
	for blockY := 0; blockY < blockRows; blockY++ {
		for blockX := 0; blockX < blockColumns; blockX++ {
			for component := 0; component < 3; component++ {
				var samples [64]uint8
				for row := 0; row < 8; row++ {
					y := blockY*8 + row
					if y >= image.height {
						y = image.height - 1
					}
					for column := 0; column < 8; column++ {
						x := blockX*8 + column
						if x >= image.width {
							x = image.width - 1
						}
						samples[row*8+column] = planes[component][y*image.width+x]
					}
				}

				coefficients := jpegForwardDCT(samples)
				quantized, err := jpegQuantizeBlock(coefficients, quantization[component])
				if err != nil {
					return nil, err
				}
				zigzag := jpegZigZag(quantized)
				tableID := 0
				if component != 0 {
					tableID = 1
				}
				dcCodewords, err := jpegEncodeDC(zigzag[0], previousDC[component], dcTables[tableID])
				if err != nil {
					return nil, err
				}
				acCodewords, err := jpegEncodeAC(zigzag, acTables[tableID])
				if err != nil {
					return nil, err
				}
				if err := writer.WriteCodewords(dcCodewords); err != nil {
					return nil, err
				}
				if err := writer.WriteCodewords(acCodewords); err != nil {
					return nil, err
				}
				previousDC[component] = zigzag[0]
			}
		}
	}
	return writer.Finish(), nil
}
