package main

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

var (
	ErrNotFound         = errors.New("resource not found")
	ErrForbidden        = errors.New("access forbidden")
	ErrMethodNotAllowed = errors.New("method not allowed")
	ErrInternal         = errors.New("internal server error")
)

type Router struct {
	fileRoot         string
	supportedMethods []string
}

// konstruktor za kreiranje novog rutera
func newRouter(fileRoot string, supportedMethods []string) *Router {
	return &Router{
		fileRoot:         fileRoot,
		supportedMethods: supportedMethods,
	}
}

// serve obradjuje dolazni zahtev i pise odgovor
func (r *Router) serve(req *Request, w *ResponseWriter) error {
	if !slices.Contains(r.supportedMethods, req.Method) {
		// 405 mora nositi Allow
		w.setHeader("Allow", strings.Join(r.supportedMethods, ", "))
		return ErrMethodNotAllowed
	}

	// absolute-path mora pocinjati sa "/"
	if !strings.HasPrefix(req.Path, "/") {
		return ErrBadRequest
	}

	trg, _, _ := strings.Cut(req.Path, "?")
	decodedPath, err := url.PathUnescape(trg)
	if err != nil {
		return ErrBadRequest
	}

	// reject NULL byte
	if strings.ContainsRune(decodedPath, '\x00') {
		return ErrBadRequest
	}

	cleanPath := path.Clean(decodedPath)

	fullPath := filepath.Join(r.fileRoot, cleanPath)

	// containment check
	rel, err := filepath.Rel(r.fileRoot, fullPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrNotFound
	}

	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return ErrForbidden
		}
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		// sve ostalo
		return fmt.Errorf("%w: %v", ErrInternal, err)
	}

	var target string
	if fileInfo.IsDir() {
		tmp := filepath.Join(fullPath, "index.html")

		tmpInfo, err := os.Stat(tmp)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return ErrNotFound
			}
			if errors.Is(err, os.ErrPermission) {
				return ErrForbidden
			}
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}

		// provera pre Open
		if !tmpInfo.Mode().IsRegular() {
			return ErrForbidden
		}

		target = tmp

	} else {
		if !fileInfo.Mode().IsRegular() {
			return ErrForbidden
		}
		target = fullPath
	}

	file, err := os.Open(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		if errors.Is(err, os.ErrPermission) {
			return ErrForbidden
		}
		return fmt.Errorf("%w: %v", ErrInternal, err)
	}

	defer file.Close()

	targetInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("%w: stat %s: %v", ErrInternal, target, err)
	}

	if !targetInfo.Mode().IsRegular() {
		return ErrForbidden
	}

	targetSize := targetInfo.Size()

	ext := filepath.Ext(target)
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	w.setHeader("Content-Type", mimeType)

	w.writeHeader(200, int(targetSize))

	if req.Method == "HEAD" {
		return nil
	}

	_, err = io.Copy(w, file)
	if err != nil {
		return fmt.Errorf("%w: copy %s: %v", ErrInternal, target, err)
	}

	return nil
}
