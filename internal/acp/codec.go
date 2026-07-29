package acp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

var errOversize = errors.New("ACP record exceeds 10 MiB")

type codec struct {
	r  *bufio.Reader
	w  io.Writer
	mu sync.Mutex
}

func newCodec(r io.Reader, w io.Writer) *codec {
	return &codec{r: bufio.NewReaderSize(r, 64<<10), w: w}
}
func (c *codec) read() (json.RawMessage, error) {
	var line []byte
	for {
		part, err := c.r.ReadSlice('\n')
		if len(line)+len(part) > MaxLine+1 {
			for err == bufio.ErrBufferFull {
				_, err = c.r.ReadSlice('\n')
			}
			return nil, errOversize
		}
		line = append(line, part...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF && len(line) > 0 {
			return bytes.TrimSuffix(line, []byte{'\r'}), nil
		}
		if err != nil {
			return nil, err
		}
		line = bytes.TrimSuffix(line, []byte{'\n'})
		line = bytes.TrimSuffix(line, []byte{'\r'})
		return line, nil
	}
}
func (c *codec) write(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	b = append(b, '\n')
	_, e = c.w.Write(b)
	return e
}
