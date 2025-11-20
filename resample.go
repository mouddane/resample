/*
	Copyright (C) 2016 - 2024, Lefteris Zafiris <zaf@fastmail.com>

	This program is free software, distributed under the terms of
	the BSD 3-Clause License. See the LICENSE file
	at the top of the source tree.
*/

/*
Package resample implements resampling of PCM-encoded audio.
It uses the SoX Resampler library `libsoxr'.

To install make sure you have libsoxr and pkg-config installed, then run:

go install github.com/zaf/resample@latest

The package warps an io.Reader in a Resampler that resamples and
writes all input data. Input should be RAW PCM encoded audio samples.

For usage details please see the code snippet in the cmd folder.
*/
package resample

/*
// Link soxr using pkg-config.
#cgo pkg-config: soxr
#include <stdlib.h>
#include <soxr.h>
*/
import "C"
import (
	"errors"
	"io"
	"runtime"
	"unsafe"
)

const (
	// Quality settings
	Quick     = 0 // Quick cubic interpolation
	LowQ      = 1 // LowQ 16-bit with larger rolloff
	MediumQ   = 2 // MediumQ 16-bit with medium rolloff
	HighQ     = 4 // High quality
	VeryHighQ = 6 // Very high quality

	// Input formats
	F32 = 0 // 32-bit floating point PCM
	F64 = 1 // 64-bit floating point PCM
	I32 = 2 // 32-bit signed linear PCM
	I16 = 3 // 16-bit signed linear PCM

	byteLen = 8
)

// Resampler resamples PCM sound data.
type Resampler struct {
	resampler    C.soxr_t
	inRate       float64   // input sample rate
	outRate      float64   // output sample rate
	channels     int       // number of input channels
	inFrameSize  int       // input frame size in bytes
	outFrameSize int       // output frame size in bytes
	destination  io.Writer // output data
}

var threads int

func init() {
	threads = runtime.NumCPU()
}

// New returns a pointer to a Resampler that implements an io.WriteCloser.
// It takes as parameters the destination data Writer, the input and output
// sampling rates, the number of channels of the input data, the input format
// and the quality setting.
func New(writer io.Writer, inputRate, outputRate float64, channels, inFormat, outFormat, quality int) (*Resampler, error) {
	var err error
	if writer == nil {
		return nil, errors.New("io.Writer is nil")
	}
	if inputRate <= 0 || outputRate <= 0 {
		return nil, errors.New("invalid input or output sampling rates")
	}
	if channels == 0 {
		return nil, errors.New("invalid channels number")
	}
	if quality < 0 || quality > 6 {
		return nil, errors.New("invalid quality setting")
	}

	// Determine byte sizes for each format
	sizeOf := func(format int) (int, error) {
		switch format {
		case F64:
			return 8, nil
		case F32:
			return 4, nil
		case I32:
			return 4, nil
		case I16:
			return 2, nil
		}
		return 0, errors.New("invalid format setting")
	}

	inSize, err := sizeOf(inFormat)
	if err != nil {
		return nil, err
	}

	outSize, err := sizeOf(outFormat)
	if err != nil {
		return nil, err
	}

	var soxr C.soxr_t
	var soxErr C.soxr_error_t
	// Setup soxr and create a stream resampler
	ioSpec := C.soxr_io_spec(C.soxr_datatype_t(inFormat), C.soxr_datatype_t(outFormat))
	qSpec := C.soxr_quality_spec(C.ulong(quality), 0)
	runtimeSpec := C.soxr_runtime_spec(C.uint(threads))

	soxr = C.soxr_create(C.double(inputRate), C.double(outputRate), C.uint(channels), &soxErr, &ioSpec, &qSpec, &runtimeSpec)
	if C.GoString(soxErr) != "" && C.GoString(soxErr) != "0" {
		err = errors.New(C.GoString(soxErr))
		C.free(unsafe.Pointer(soxErr))
		return nil, err
	}

	r := Resampler{
		resampler:    soxr,
		inRate:       inputRate,
		outRate:      outputRate,
		channels:     channels,
		inFrameSize:  inSize,
		outFrameSize: outSize,
		destination:  writer,
	}
	C.free(unsafe.Pointer(soxErr))
	return &r, err
}

// Reset permits reusing a Resampler rather than allocating a new one.
func (r *Resampler) Reset(writer io.Writer) error {
	var err error
	if r.resampler == nil {
		return errors.New("soxr resampler is nil")
	}
	err = r.flush()
	r.destination = writer
	C.soxr_clear(r.resampler)
	return err
}

// Close flushes, clean-ups and frees memory. Should always be called when
// finished using the resampler. Should always be called when finished using
// the resampler, and before we can use its output.
func (r *Resampler) Close() error {
	var err error
	if r.resampler == nil {
		return errors.New("soxr resampler is nil")
	}
	err = r.flush()
	C.soxr_delete(r.resampler)
	r.resampler = nil
	return err
}

// Write resamples PCM sound data. Writes len(p) bytes from p to
// the underlying data stream, returns the number of bytes written
// from p (0 <= n <= len(p)) and any error encountered that caused
// the write to stop early.
func (r *Resampler) Write(p []byte) (int, error) {
	var err error
	var i int
	if r.resampler == nil {
		return i, errors.New("soxr resampler is nil")
	}
	if len(p) == 0 {
		return i, nil
	}
	framesIn := len(p) / r.inFrameSize / r.channels
	if framesIn == 0 {
		return i, errors.New("incomplete input frame data")
	}
	framesOut := int(float64(framesIn) * (r.outRate / r.inRate))
	if framesOut == 0 {
		return i, errors.New("not enough input to generate output")
	}
	dataIn := C.CBytes(p)
	dataOut := C.malloc(C.size_t(framesOut * r.channels * r.outFrameSize))
	var soxErr C.soxr_error_t
	var read, done C.size_t = 0, 0
	soxErr = C.soxr_process(r.resampler, C.soxr_in_t(dataIn), C.size_t(framesIn), &read, C.soxr_out_t(dataOut), C.size_t(framesOut), &done)
	if C.GoString(soxErr) != "" && C.GoString(soxErr) != "0" {
		err = errors.New(C.GoString(soxErr))
		goto cleanup
	}
	_, err = r.destination.Write(C.GoBytes(dataOut, C.int(int(done)*r.channels*r.outFrameSize)))
	// In many cases the resampler will not return the full data unless we flush it. Espasially if the input chunck is small
	// As long as we close the resampler (Close() flushes all data) we don't need to worry about short writes, unless r.destination.Write() fails
	if err == nil {
		i = len(p)
	}
cleanup:
	C.free(dataIn)
	C.free(dataOut)
	C.free(unsafe.Pointer(soxErr))
	return i, err
}

// flush any pending output from the resampler. Aftter that no more input can be passed.
func (r *Resampler) flush() error {
	var err error
	var done C.size_t
	var soxErr C.soxr_error_t
	framesOut := 4096 * 16
	dataOut := C.malloc(C.size_t(framesOut * r.channels * r.outFrameSize))
	// Flush any pending output by calling soxr_process with no input data.
	soxErr = C.soxr_process(r.resampler, nil, 0, nil, C.soxr_out_t(dataOut), C.size_t(framesOut), &done)
	if C.GoString(soxErr) != "" && C.GoString(soxErr) != "0" {
		err = errors.New(C.GoString(soxErr))
		goto cleanup
	}
	_, err = r.destination.Write(C.GoBytes(dataOut, C.int(int(done)*r.channels*r.outFrameSize)))
cleanup:
	C.free(dataOut)
	C.free(unsafe.Pointer(soxErr))
	return err
}
