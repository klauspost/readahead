//go:build go1.25

package readahead_test

import (
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"testing/synctest"

	"github.com/klauspost/readahead"
)

type closeCountingReader struct {
	io.ReadCloser
	closes int
}

func (r *closeCountingReader) Close() error {
	r.closes++
	return r.ReadCloser.Close()
}

func TestReadCloserCloseWithBlockedForegroundRead(t *testing.T) {
	// Go 1.17 modules default to legacy timers; synctest needs synchronous timers.
	t.Setenv("GODEBUG", os.Getenv("GODEBUG")+",asynctimerchan=0")
	for _, writeTo := range []bool{false, true} {
		t.Run(fmt.Sprint("WriteTo=", writeTo), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				conn, peer := net.Pipe()
				defer conn.Close()
				defer peer.Close()
				input := &closeCountingReader{ReadCloser: conn}
				r, err := readahead.NewReadCloserSize(input, 2, 8)
				if err != nil {
					t.Fatal(err)
				}
				result := make(chan error, 1)
				go func() {
					var err error
					if writeTo {
						_, err = r.(io.WriterTo).WriteTo(io.Discard)
					} else {
						_, err = r.Read(make([]byte, 1))
					}
					result <- err
				}()

				// All channels, including net.Pipe's, belong to this bubble.
				// The foreground call must be blocked before Close can run.
				synctest.Wait()
				select {
				case err := <-result:
					t.Fatalf("foreground operation completed before Close: %v", err)
				default:
				}

				for i := 0; i < 3; i++ {
					if err := r.Close(); err != nil {
						t.Fatal(err)
					}
				}
				synctest.Wait()
				select {
				case err := <-result:
					if err != io.ErrClosedPipe {
						t.Fatalf("foreground operation: got %v, want %v", err, io.ErrClosedPipe)
					}
				default:
					t.Fatal("Close did not release the pending foreground operation")
				}
				if input.closes != 1 {
					t.Fatalf("underlying close count: got %d, want 1", input.closes)
				}
			})
		})
	}
}
