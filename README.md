# readahead
Asynchronous readahead for Go readers

This package will allow you to give any reader, and a separate goroutine will perform reads from your upstream reader, so you can request from this reader without delay.

This is helpful for splitting an input stream into concurrent processing.


[![GoDoc][1]][2] [![Build Status][3]][4]

[1]: https://godoc.org/github.com/klauspost/readahead?status.svg
[2]: https://godoc.org/github.com/klauspost/readahead
[3]: https://travis-ci.org/klauspost/readahead.svg
[4]: https://travis-ci.org/klauspost/readahead

# features

This should be fully transparent, except that once an error has been returned from the Reader, it will not recover.

The readahead object also fulfills the io.WriterTo interface, which is likely to speed up copies.

# usage

To get the package use `go get -u github.com/klauspost/readahead`.

Here is a simple example that does file copy. Error checkeing has been omitted for brevity.
```Go
input, _ := os.Open("input.txt")
output, _ := os.Create("output.txt")
defer input.Close()
defer output.Close()

// Create a Reader with default settings
reader := readahead.NewReader(input)
defer reader.Close()

// Copy the content to our output
io.Copy(dst, reader)
```

# settings

You can finetune

# license

This package is released under the MIT license. See the supplied LICENSE file for more info.
