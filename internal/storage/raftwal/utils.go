package raftwal

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/golang/glog"
)

// Sensitive implements the Stringer interface to redact its contents.
// Use this type for sensitive info such as keys, passwords, or secrets so it doesn't leak
// as output such as logs.
type Sensitive []byte

func (Sensitive) String() string {
	return "****"
}

func AssertTrue(b bool) {
	if !b {
		log.Fatalf("%+v", fmt.Errorf("Assert failed"))
	}
}

// Check logs fatal if err != nil.
func Check(err error) {
	if err != nil {
		log.Fatalf("%+v", err)
	}
}

// WalkPathFunc walks the directory 'dir' and collects all path names matched by
// func f. If the path is a directory, it will set the bool argument to true.
// Returns empty string slice if nothing found, otherwise returns all matched path names.
func WalkPathFunc(dir string, f func(string, bool) bool) []string {
	var list []string
	err := filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if f(path, fi.IsDir()) {
			list = append(list, path)
		}
		return nil
	})
	if err != nil {
		glog.Errorf("Error while scanning %q: %s", dir, err)
	}
	return list
}

// Copy copies a byte slice and returns the copied slice.
func Copy(a []byte) []byte {
	b := make([]byte, len(a))
	copy(b, a)
	return b
}

func XORBlockAllocate(src, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	stream := cipher.NewCTR(block, iv)
	dst := make([]byte, len(src))
	stream.XORKeyStream(dst, src)
	return dst, nil
}

func XORBlockStream(w io.Writer, src, key, iv []byte) error {
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	stream := cipher.NewCTR(block, iv)
	sw := cipher.StreamWriter{S: stream, W: w}
	_, err = io.Copy(sw, bytes.NewReader(src))
	return Wrapf(err, "XORBlockStream")
}

// Wrapf is Wrap with extra info.
func Wrapf(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(format+" error: %+v", append(args, err)...)
}

// AssertTruef is AssertTrue with extra info.
func AssertTruef(b bool, format string, args ...interface{}) {
	if !b {
		log.Fatalf("%+v", fmt.Errorf(format, args...))
	}
}
