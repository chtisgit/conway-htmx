package main

import (
	"bytes"
	"io"
)

func stripNewlines(w io.Writer) io.Writer {
	return &newlineStripper{w: w}
}

type newlineStripper struct {
	w io.Writer
	b bytes.Buffer
}

var _ io.Writer = (*newlineStripper)(nil)

func (ns *newlineStripper) Write(p []byte) (int, error) {
	for i := range p {
		if p[i] != '\n' && p[i] != '\r' {
			ns.b.WriteByte(p[i])
		}
	}

	nwr, err := ns.b.WriteTo(ns.w)
	ns.b.Reset()
	if err != nil {
		// conversion of nwr is safe as of WriteTo documentation.
		// unfortunately, this is not completely correct, because what should be
		// returned is the number of characters that would have been written before
		// we replaced the newlines.
		return int(nwr), err
	}

	return len(p), nil
}
