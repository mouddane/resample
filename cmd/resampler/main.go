/*
	Copyright (C) 2016 - 2024, Lefteris Zafiris <zaf@fastmail.com>

	This program is free software, distributed under the terms of
	the BSD 3-Clause License. See the LICENSE file
	at the top of the source tree.
*/

// The program takes as input a WAV or RAW PCM sound file
// and resamples it to the desired sampling rate.
// The output is RAW PCM data.
// Usage: goresample [flags] input_file output_file
//
// Example: go run main.go -ir 16000 -or 8000 ../../testing/piano-16k-16-2.wav 8k.raw

package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/zaf/resample"
)

const wavHeader = 44

var (
	inFormat  = flag.String("if", "i16", "PCM input format")
	outFormat = flag.String("iof", "i16", "PCM output format")
	ch        = flag.Int("ch", 2, "Number of channels")
	ir        = flag.Int("ir", 44100, "Input sample rate")
	or        = flag.Int("or", 0, "Output sample rate")
)

func strToFormat(format string) (int, error) {
	switch strings.ToLower(format) {
	case "i16":
		return resample.I16, nil
	case "i32":
		return resample.I32, nil
	case "f32":
		return resample.F32, nil
	case "f64":
		return resample.F64, nil
	}
	return 0, fmt.Errorf("unknown format %s", format)
}

func main() {
	flag.Parse()
	inFrmt, err := strToFormat(*inFormat)
	if err != nil {
		log.Fatalf("Invalid input format : %s", err)
	}
	outFrmt, err := strToFormat(*outFormat)
	if err != nil {
		log.Fatalf("Invalid output format : %s", err)
	}
	if *ch < 1 {
		log.Fatalln("Invalid channel number")
	}
	if *ir <= 0 || *or <= 0 {
		log.Fatalln("Invalid input or output sample rate")
	}
	if flag.NArg() < 2 {
		log.Fatalln("No input or output files given")
	}
	inputFile := flag.Arg(0)
	outputFile := flag.Arg(1)

	// Open input file (WAV or RAW PCM)
	input, err := os.Open(inputFile)
	if err != nil {
		log.Fatalln(err)
	}
	defer input.Close()
	output, err := os.Create(outputFile)
	if err != nil {
		log.Fatalln(err)
	}
	// Create a Resampler
	res, err := resample.New(output, float64(*ir), float64(*or), *ch, inFrmt, outFrmt, resample.HighQ)
	if err != nil {
		output.Close()
		os.Remove(outputFile)
		log.Fatalln(err)
	}
	// Skip WAV file header in order to pass only the PCM data to the Resampler
	if strings.ToLower(filepath.Ext(inputFile)) == ".wav" {
		input.Seek(wavHeader, 0)
	}

	// Read input and pass it to the Resampler in chunks
	_, err = io.Copy(res, input)
	// Close the Resampler and the output file. Clsoing the Resampler will flush any remaining data to the output file.
	// If the Resampler is not closed before the output file, any remaining data will be lost.
	res.Close()
	output.Close()
	if err != nil {
		os.Remove(outputFile)
		log.Fatalln(err)
	}
}
