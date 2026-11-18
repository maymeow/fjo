package a

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
)

func files(f *os.File) {
	fmt.Fprintf(f, "x %d", 1) // want `fmt.Fprintf writes to a file \(\*os.File\)`
	fmt.Fprintln(f, "x")      // want `fmt.Fprintln writes to a file`
	_, _ = fmt.Fprint(f, "x") // want `fmt.Fprint writes to a file`

	fmt.Fprintln(os.Stdout, "ok")
	fmt.Fprintln(os.Stderr, "ok")
	fmt.Fprintln((os.Stderr), "ok")
}

func network(c net.Conn, tc *net.TCPConn, w http.ResponseWriter) {
	fmt.Fprintf(c, "x")  // want `fmt.Fprintf writes to a network connection \(net.Conn\)`
	fmt.Fprintf(tc, "x") // want `fmt.Fprintf writes to a network connection`
	fmt.Fprintf(w, "x")  // want `fmt.Fprintf writes to an HTTP response \(http.ResponseWriter\)`
}

func allowed(w io.Writer, f *os.File) error {
	var buf bytes.Buffer
	var sb strings.Builder
	bw := bufio.NewWriter(f)

	fmt.Fprintf(&buf, "x")
	fmt.Fprintf(&sb, "x")
	fmt.Fprintf(bw, "x")
	fmt.Fprintf(w, "x")
	return bw.Flush()
}
