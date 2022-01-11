package readahead_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"sync"
	"testing"
	"testing/iotest"

	"github.com/klauspost/readahead"
)

func TestReader(t *testing.T) {
	buf := bytes.NewBufferString("Testbuffer")
	ar, err := readahead.NewReaderSize(buf, 4, 10000)
	if err != nil {
		t.Fatal("error when creating:", err)
	}

	var dst = make([]byte, 100)
	n, err := ar.Read(dst)
	if err != nil {
		t.Fatal("error when reading:", err)
	}
	if n != 10 {
		t.Fatal("unexpected length, expected 10, got ", n)
	}

	n, err = ar.Read(dst)
	if err != io.EOF {
		t.Fatal("expected io.EOF, got", err)
	}
	if n != 0 {
		t.Fatal("unexpected length, expected 0, got ", n)
	}

	// Test read after error
	n, err = ar.Read(dst)
	if err != io.EOF {
		t.Fatal("expected io.EOF, got", err)
	}
	if n != 0 {
		t.Fatal("unexpected length, expected 0, got ", n)
	}

	err = ar.Close()
	if err != nil {
		t.Fatal("error when closing:", err)
	}

	// Test Close without reading everything
	buf = bytes.NewBuffer(make([]byte, 50000))
	ar, err = readahead.NewReaderSize(buf, 4, 100)
	if err != nil {
		t.Fatal("error when creating:", err)
	}
	err = ar.Close()
	if err != nil {
		t.Fatal("error when closing:", err)
	}

	// Test Read after close
	_, err = ar.Read(make([]byte, 50000))
	if err == nil {
		t.Fatal("want error when closing, got:", err)
	}
}

type SeekerBuffer struct {
	*bytes.Buffer
	pos int64
}

func (s *SeekerBuffer) Read(p []byte) (n int, err error) {
	n, err = bytes.NewReader(s.Bytes()[s.pos:]).Read(p)
	if n > 0 {
		s.pos += int64(n)
	}
	return
}

func (s *SeekerBuffer) Seek(offset int64, whence int) (res int64, err error) {
	if offset > int64(s.Len()) {
		err = fmt.Errorf("wrong offset")
		return
	}
	switch whence {
	case io.SeekStart:
		res = offset
	case io.SeekCurrent:
		res = s.pos + offset
	case io.SeekEnd:
		res = int64(s.Len()) + offset
	}
	s.pos = res
	return
}

func TestSeeker(t *testing.T) {
	testBytes := []byte("Testbuffer")
	newControl := func(i int) io.Reader {
		buf := bytes.NewBuffer(testBytes)
		for j := 0; j < i*100-1; j++ {
			buf.Write(testBytes)
		}
		return buf
	}
	for i := 1; i <= 100; i++ {
		length := len(testBytes) * i * 100
		buf := &SeekerBuffer{
			Buffer: bytes.NewBuffer(testBytes),
		}
		for j := 0; j < i*100-1; j++ {
			buf.Write(testBytes)
		}
		control := newControl(i)
		ar, err := readahead.NewReadSeekerSize(buf, i, 11*i)
		if _, ok := control.(io.Seeker); ok {
			t.Fatal("created reader implements seeking without underlying reader support")
		}
		if err != nil {
			t.Fatal("error when creating:", err)
		}
		dstSize := 3 * i
		dst := make([]byte, dstSize)
		controlDst := make([]byte, dstSize)
		control.Read(controlDst)
		n, err := ar.Read(dst)
		if err != nil {
			t.Fatal("error when reading:", err)
		}
		if n != dstSize {
			t.Fatal("unexpected length, expected ", dstSize, ", got ", n)
		}
		if string(dst) != string(controlDst) {
			t.Fatal("seeker and control reader mismatch")
		}

		pos, err := ar.Seek(1, io.SeekStart)
		if err != nil {
			t.Fatal("error when seeking:", err)
		}
		if pos != 1 {
			t.Fatal("unexpected position, expected 1, got ", pos)
		}
		control = newControl(i)
		control.Read(make([]byte, 1)) //Emulate seeking to offset 1 from beginning
		control.Read(controlDst)
		n, err = ar.Read(dst)
		if err != nil {
			t.Fatal("error when reading:", err)
		}
		if n != dstSize {
			t.Fatal("unexpected length, expected ", dstSize, ", got ", n)
		}
		if string(dst) != string(controlDst) {
			t.Fatal("seeker and control reader mismatch")
		}

		pos, err = ar.Seek(int64(i), io.SeekCurrent)
		if err != nil {
			t.Fatal("error when seeking:", err)
		}
		if pos != int64(dstSize+i+1) {
			t.Fatal("unexpected position, expected ", dstSize+i, ", got ", pos)
		}
		control.Read(make([]byte, int64(i))) //Emulate seeking to offset 1 from current pos
		control.Read(controlDst)
		n, err = ar.Read(dst)
		if err != nil {
			t.Fatal("error when reading:", err)
		}
		if n != dstSize {
			t.Fatal("unexpected length, expected ", dstSize, ", got ", n)
		}
		if string(dst) != string(controlDst) {
			t.Fatal("seeker and control reader mismatch")
		}

		control = newControl(i)
		pos, err = ar.Seek(-1, io.SeekEnd)
		if err != nil {
			t.Fatal("error when seeking:", err)
		}
		if pos != int64(length-1) {
			t.Fatal("unexpected position, expected ", length-1, ", got ", pos)
		}
		control.Read(make([]byte, length-1)) //Emulate seeking to offset -1 from the end
		control.Read(controlDst)
		n, err = ar.Read(dst)
		if err != nil {
			t.Fatal("error when reading:", err)
		}
		if n != 1 {
			t.Fatal("unexpected length, expected 1, got ", n)
		}
		if string(dst[:n]) != string(controlDst[:n]) {
			t.Fatal("seeker and control reader mismatch")
		}

		n, err = ar.Read(dst)
		if err != io.EOF {
			t.Fatal("expected io.EOF, got", err)
		}
		if n != 0 {
			t.Fatal("unexpected length, expected 0, got ", n)
		}
	}
}

type testCloser struct {
	io.Reader
	closed  int
	onClose error
}

func (t *testCloser) Close() error {
	t.closed++
	return t.onClose
}

func TestReadCloser(t *testing.T) {
	buf := bytes.NewBufferString("Testbuffer")
	cl := &testCloser{Reader: buf}
	ar, err := readahead.NewReadCloserSize(cl, 4, 10000)
	if err != nil {
		t.Fatal("error when creating:", err)
	}

	var dst = make([]byte, 100)
	n, err := ar.Read(dst)
	if err != nil {
		t.Fatal("error when reading:", err)
	}
	if n != 10 {
		t.Fatal("unexpected length, expected 10, got ", n)
	}

	n, err = ar.Read(dst)
	if err != io.EOF {
		t.Fatal("expected io.EOF, got", err)
	}
	if n != 0 {
		t.Fatal("unexpected length, expected 0, got ", n)
	}

	// Test read after error
	n, err = ar.Read(dst)
	if err != io.EOF {
		t.Fatal("expected io.EOF, got", err)
	}
	if n != 0 {
		t.Fatal("unexpected length, expected 0, got ", n)
	}

	err = ar.Close()
	if err != nil {
		t.Fatal("error when closing:", err)
	}
	if cl.closed != 1 {
		t.Fatal("want close count 1, got:", cl.closed)
	}
	// Test Close without reading everything
	buf = bytes.NewBuffer(make([]byte, 50000))
	cl = &testCloser{Reader: buf}
	ar = readahead.NewReadCloser(cl)
	err = ar.Close()
	if err != nil {
		t.Fatal("error when closing:", err)
	}
	if cl.closed != 1 {
		t.Fatal("want close count 1, got:", cl.closed)
	}
	// Test error forwarding
	cl = &testCloser{Reader: buf, onClose: errors.New("an error")}
	ar = readahead.NewReadCloser(cl)
	err = ar.Close()
	if err != cl.onClose {
		t.Fatal("want error when closing, got", err)
	}
	if cl.closed != 1 {
		t.Fatal("want close count 1, got:", cl.closed)
	}
	// Test multiple closes
	cl = &testCloser{Reader: buf}
	ar = readahead.NewReadCloser(cl)
	err = ar.Close()
	if err != nil {
		t.Fatal("error when closing:", err)
	}
	err = ar.Close()
	if err != nil {
		t.Fatal("error when closing:", err)
	}
	if cl.closed != 1 {
		t.Fatal("want close count 1, got:", cl.closed)
	}
}

func TestWriteTo(t *testing.T) {
	buf := bytes.NewBufferString("Testbuffer")
	ar, err := readahead.NewReaderSize(buf, 4, 10000)
	if err != nil {
		t.Fatal("error when creating:", err)
	}

	var dst = &bytes.Buffer{}
	n, err := io.Copy(dst, ar)
	// A successful Copy returns err == nil, not err == EOF.
	// Because Copy is defined to read from src until EOF,
	// it does not treat an EOF from Read as an error to be reported.
	if err != nil {
		t.Fatal("error when reading:", err)
	}
	if n != 10 {
		t.Fatal("unexpected length, expected 10, got ", n)
	}

	// Should still return EOF
	n, err = io.Copy(dst, ar)
	if err != io.EOF {
		t.Fatal("expected io.EOF, got", err)
	}
	if n != 0 {
		t.Fatal("unexpected length, expected 0, got ", n)
	}

	err = ar.Close()
	if err != nil {
		t.Fatal("error when closing:", err)
	}
	// Test Read after close
	_, err = io.Copy(dst, ar)
	if err == nil {
		t.Fatal("want error when closing, got:", err)
	}
}

func TestNilReader(t *testing.T) {
	ar := readahead.NewReader(nil)
	if ar != nil {
		t.Fatalf("expected a nil, got %#v", ar)
	}
	buf := bytes.NewBufferString("Testbuffer")
	ar = readahead.NewReader(buf)
	if ar == nil {
		t.Fatalf("got nil when expecting object")
	}
}

func TestReaderErrors(t *testing.T) {
	// test nil reader
	_, err := readahead.NewReaderSize(nil, 4, 10000)
	if err == nil {
		t.Fatal("expected error when creating, but got nil")
	}

	// invalid buffer number
	buf := ioutil.NopCloser(bytes.NewBufferString("Testbuffer"))
	_, err = readahead.NewReaderSize(buf, 0, 10000)
	if err == nil {
		t.Fatal("expected error when creating, but got nil")
	}
	_, err = readahead.NewReaderSize(buf, -1, 10000)
	if err == nil {
		t.Fatal("expected error when creating, but got nil")
	}

	// invalid buffer size
	_, err = readahead.NewReaderSize(buf, 4, 0)
	if err == nil {
		t.Fatal("expected error when creating, but got nil")
	}
	_, err = readahead.NewReaderSize(buf, 4, -1)
	if err == nil {
		t.Fatal("expected error when creating, but got nil")
	}
}

// Complex read tests, leveraged from "bufio".

type readMaker struct {
	name string
	fn   func(io.Reader) io.Reader
}

var readMakers = []readMaker{
	{"full", func(r io.Reader) io.Reader { return r }},
	{"byte", iotest.OneByteReader},
	{"half", iotest.HalfReader},
	{"data+err", iotest.DataErrReader},
	{"timeout", iotest.TimeoutReader},
}

// Call Read to accumulate the text of a file
func reads(buf io.Reader, m int) string {
	var b [1000]byte
	nb := 0
	for {
		n, err := buf.Read(b[nb : nb+m])
		nb += n
		if err == io.EOF {
			break
		} else if err != nil && err != iotest.ErrTimeout {
			panic("Data: " + err.Error())
		} else if err != nil {
			break
		}
	}
	return string(b[0:nb])
}

type dummyReader struct {
	readFN func([]byte) (int, error)
}

func (d dummyReader) Read(dst []byte) (int, error) {
	return d.readFN(dst)
}

func TestReaderPanic(t *testing.T) {
	r := dummyReader{readFN: func(dst []byte) (int, error) {
		panic("some underlying panic")
	}}
	reader := readahead.NewReader(r)
	defer reader.Close()

	// Copy the content to dst
	var dst = &bytes.Buffer{}
	_, err := io.Copy(dst, reader)
	if err == nil {
		t.Fatal("Want error, got nil")
	}
}

func TestReaderLatePanic(t *testing.T) {
	var n int
	var mu sync.Mutex
	r := dummyReader{readFN: func(dst []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		if n >= 10 {
			panic("some underlying panic")
		}
		n++
		return len(dst), nil
	}}
	reader := readahead.NewReader(r)
	defer reader.Close()

	// Copy the content to dst
	var dst = &bytes.Buffer{}
	_, err := io.Copy(dst, reader)
	if err == nil {
		t.Fatal("Want error, got nil")
	}
	mu.Lock()
	if n < 10 {
		t.Fatalf("Want at least 10 calls, got %v", n)
	}
	mu.Unlock()
}

func TestReaderLateError(t *testing.T) {
	var n int
	var mu sync.Mutex
	theErr := errors.New("some error")
	r := dummyReader{readFN: func(dst []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		if n >= 10 {
			return 0, theErr
		}
		n++
		return len(dst), nil
	}}
	reader := readahead.NewReader(r)
	defer reader.Close()

	// Copy the content to dst
	var dst = &bytes.Buffer{}
	_, err := io.Copy(dst, reader)
	if err != theErr {
		t.Fatalf("Want %#v, got %#v", theErr, err)
	}
	mu.Lock()
	if n < 10 {
		t.Fatalf("Want at least 10 calls, got %v", n)
	}
	mu.Unlock()
}

type bufReader struct {
	name string
	fn   func(io.Reader) string
}

var bufreaders = []bufReader{
	{"1", func(b io.Reader) string { return reads(b, 1) }},
	{"2", func(b io.Reader) string { return reads(b, 2) }},
	{"3", func(b io.Reader) string { return reads(b, 3) }},
	{"4", func(b io.Reader) string { return reads(b, 4) }},
	{"5", func(b io.Reader) string { return reads(b, 5) }},
	{"7", func(b io.Reader) string { return reads(b, 7) }},
}

const minReadBufferSize = 16

var bufsizes = []int{
	0, minReadBufferSize, 23, 32, 46, 64, 93, 128, 1024, 4096,
}

// Test various  input buffer sizes, number of buffers and read sizes.
func TestReaderSizes(t *testing.T) {
	var texts [31]string
	str := ""
	all := ""
	for i := 0; i < len(texts)-1; i++ {
		texts[i] = str + "\n"
		all += texts[i]
		str += string(byte(i%26) + 'a')
	}
	texts[len(texts)-1] = all

	for h := 0; h < len(texts); h++ {
		text := texts[h]
		for i := 0; i < len(readMakers); i++ {
			for j := 0; j < len(bufreaders); j++ {
				for k := 0; k < len(bufsizes); k++ {
					for l := 1; l < 10; l++ {
						readmaker := readMakers[i]
						bufreader := bufreaders[j]
						bufsize := bufsizes[k]
						read := readmaker.fn(strings.NewReader(text))
						buf := bufio.NewReaderSize(read, bufsize)
						ar, _ := readahead.NewReaderSize(buf, l, 100)
						s := bufreader.fn(ar)
						// "timeout" expects the Reader to recover, asyncReader does not.
						if s != text && readmaker.name != "timeout" {
							t.Errorf("reader=%s fn=%s bufsize=%d want=%q got=%q",
								readmaker.name, bufreader.name, bufsize, text, s)
						}
						err := ar.Close()
						if err != nil {
							t.Fatal("Unexpected close error:", err)
						}
					}
				}
			}
		}
	}
}

// Test various input buffer sizes, number of buffers and read sizes.
func TestReaderWriteTo(t *testing.T) {
	var texts [31]string
	str := ""
	all := ""
	for i := 0; i < len(texts)-1; i++ {
		texts[i] = str + "\n"
		all += texts[i]
		str += string(byte(i%26) + 'a')
	}
	texts[len(texts)-1] = all

	for h := 0; h < len(texts); h++ {
		text := texts[h]
		for i := 0; i < len(readMakers); i++ {
			for j := 0; j < len(bufreaders); j++ {
				for k := 0; k < len(bufsizes); k++ {
					for l := 1; l < 10; l++ {
						readmaker := readMakers[i]
						bufreader := bufreaders[j]
						bufsize := bufsizes[k]
						read := readmaker.fn(strings.NewReader(text))
						buf := bufio.NewReaderSize(read, bufsize)
						ar, _ := readahead.NewReaderSize(buf, l, 100)
						dst := &bytes.Buffer{}
						wt := ar.(io.WriterTo)
						_, err := wt.WriteTo(dst)
						if err != nil && err != iotest.ErrTimeout {
							t.Fatal("Copy:", err)
						}
						s := dst.String()
						// "timeout" expects the Reader to recover, asyncReader does not.
						if s != text && readmaker.name != "timeout" {
							t.Errorf("reader=%s fn=%s bufsize=%d want=%q got=%q",
								readmaker.name, bufreader.name, bufsize, text, s)
						}
						err = ar.Close()
						if err != nil {
							t.Fatal("Unexpected close error:", err)
						}
					}
				}
			}
		}
	}
}

func Example() {
	// Buf is our input.
	buf := bytes.NewBufferString("Example data")

	// Create a Reader with default settings
	reader := readahead.NewReader(buf)
	defer reader.Close()

	// Copy the content to dst
	var dst = &bytes.Buffer{}
	_, err := io.Copy(dst, reader)
	if err != nil {
		panic("error when reading: " + err.Error())
	}

	fmt.Println(dst.String())
	// Output: Example data
}
