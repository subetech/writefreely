/*
 * Copyright © 2026 Musing Studio LLC.
 *
 * This file is part of WriteFreely.
 *
 * WriteFreely is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License, included
 * in the LICENSE file in this source code package.
 */

package writefreely

import (
	"bytes"
	"errors"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/writeas/impart"
	"github.com/writeas/web-core/id"
	"github.com/writeas/web-core/log"
)

const (
	mediaDir = "media"

	// mediaMaxFileSize is the largest image that can be uploaded.
	mediaMaxFileSize = 20 << 20
	// mediaMaxRequestSize is the largest upload request we'll read, which may
	// contain several files.
	mediaMaxRequestSize = 100 << 20
	mediaMaxFilesPerReq = 20

	mediaNameLen = 16
)

var (
	// Only names we generate ourselves are ever served, which also rules out
	// path traversal, hidden/temporary files, and directory listings.
	mediaFilenameReg = regexp.MustCompile(`^[A-Za-z0-9]{1,64}\.(jpg|png|gif|webp)$`)

	// Allowed image types, as detected from the file contents, mapped to
	// their file extension. SVG is intentionally not allowed, as it can
	// contain scripts.
	mediaTypeExts = map[string]string{
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/gif":  ".gif",
		"image/webp": ".webp",
	}
	mediaExtTypes = map[string]string{
		".jpg":  "image/jpeg",
		".png":  "image/png",
		".gif":  "image/gif",
		".webp": "image/webp",
	}

	errMediaNotFound    = impart.HTTPError{Status: http.StatusNotFound, Message: "Media not found."}
	errMediaBadType     = impart.HTTPError{Status: http.StatusUnsupportedMediaType, Message: "Only JPEG, PNG, GIF and WebP images can be uploaded."}
	errMediaTooLarge    = impart.HTTPError{Status: http.StatusRequestEntityTooLarge, Message: "Image is too large. The maximum size is 20 MB."}
	errMediaNoFile      = impart.HTTPError{Status: http.StatusBadRequest, Message: "No image given. Upload images in the `file` form field."}
	errMediaTooMany     = impart.HTTPError{Status: http.StatusBadRequest, Message: "Too many images in one request."}
	errMediaCSRF        = impart.HTTPError{Status: http.StatusForbidden, Message: "Missing X-Requested-With header."}
	errMediaWriteFailed = impart.HTTPError{Status: http.StatusInternalServerError, Message: "Couldn't save image. Please try again."}
)

// MediaFile is an uploaded image.
type MediaFile struct {
	Name     string    `json:"name"`
	URL      string    `json:"url"`
	Markdown string    `json:"markdown"`
	HTML     string    `json:"html"`
	Size     int64     `json:"size"`
	Created  time.Time `json:"created"`
}

func (app *App) mediaPath() string {
	return filepath.Join(app.cfg.Server.MediaParentDir, mediaDir)
}

func (app *App) newMediaFile(name string, size int64, created time.Time) MediaFile {
	u := strings.TrimRight(app.cfg.App.Host, "/") + "/" + mediaDir + "/" + name
	return MediaFile{
		Name:     name,
		URL:      u,
		Markdown: "![](" + u + ")",
		HTML:     `<img src="` + html.EscapeString(u) + `" alt="" />`,
		Size:     size,
		Created:  created.UTC(),
	}
}

// handleViewMedia serves a single uploaded image at /media/{filename}.
// Directories are never listed.
func handleViewMedia(app *App, w http.ResponseWriter, r *http.Request) error {
	name := mux.Vars(r)["filename"]
	if !mediaFilenameReg.MatchString(name) {
		return errMediaNotFound
	}

	f, err := os.Open(filepath.Join(app.mediaPath(), name))
	if err != nil {
		if !os.IsNotExist(err) {
			log.Error("Unable to open media file %s: %v", name, err)
		}
		return errMediaNotFound
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return errMediaNotFound
	}

	w.Header().Set("Content-Type", mediaExtTypes[filepath.Ext(name)])
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	// File names are random and never reused, so they can be cached forever
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, name, fi.ModTime(), f)
	return nil
}

// handleUploadMedia saves images uploaded in the `file` field(s) of a
// multipart form, returning info about each saved image.
func handleUploadMedia(app *App, u *User, w http.ResponseWriter, r *http.Request) error {
	// Uploads authenticated via cookies must come from our own JS: browsers
	// won't let other sites set this header without a CORS preflight, which
	// we don't allow. This protects against CSRF.
	if r.Header.Get("Authorization") == "" && r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
		return errMediaCSRF
	}

	silenced, err := app.db.IsUserSilenced(u.ID)
	if err != nil {
		log.Error("upload media: get user status: %v", err)
		return ErrInternalGeneral
	}
	if silenced {
		return ErrUserSilenced
	}

	r.Body = http.MaxBytesReader(w, r.Body, mediaMaxRequestSize)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return errMediaTooLarge
		}
		return errMediaNoFile
	}
	defer r.MultipartForm.RemoveAll()

	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		return errMediaNoFile
	}
	if len(files) > mediaMaxFilesPerReq {
		return errMediaTooMany
	}

	if err := os.MkdirAll(app.mediaPath(), 0755); err != nil {
		log.Error("Unable to create media directory: %v", err)
		return errMediaWriteFailed
	}

	res := []MediaFile{}
	for _, fh := range files {
		if fh.Size > mediaMaxFileSize {
			return errMediaTooLarge
		}
		mf, err := app.saveMediaFile(fh)
		if err != nil {
			return err
		}
		log.Info("User %d uploaded media %s (%d bytes)", u.ID, mf.Name, mf.Size)
		res = append(res, *mf)
	}
	return impart.WriteSuccess(w, res, http.StatusCreated)
}

func (app *App) saveMediaFile(fh *multipart.FileHeader) (*MediaFile, error) {
	src, err := fh.Open()
	if err != nil {
		return nil, errMediaNoFile
	}
	defer src.Close()

	// Detect the file type from its contents, not its name or the
	// client-provided Content-Type.
	head := make([]byte, 512)
	n, err := io.ReadFull(src, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, errMediaNoFile
	}
	head = head[:n]
	ext, ok := mediaTypeExts[http.DetectContentType(head)]
	if !ok {
		return nil, errMediaBadType
	}

	// Write to a temporary file first (which is never served), then move it
	// into place, so a partially-written image is never visible.
	tmp, err := os.CreateTemp(app.mediaPath(), ".upload-*")
	if err != nil {
		log.Error("Unable to create temp media file: %v", err)
		return nil, errMediaWriteFailed
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed

	size, err := io.Copy(tmp, io.MultiReader(bytes.NewReader(head), io.LimitReader(src, mediaMaxFileSize)))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		log.Error("Unable to write media file: %v", err)
		return nil, errMediaWriteFailed
	}
	if size > mediaMaxFileSize {
		return nil, errMediaTooLarge
	}
	if err := os.Chmod(tmpName, 0644); err != nil {
		log.Error("Unable to chmod media file: %v", err)
	}

	for i := 0; i < 5; i++ {
		name := id.Generate62RandomString(mediaNameLen) + ext
		dst := filepath.Join(app.mediaPath(), name)
		if _, err := os.Lstat(dst); err == nil {
			// Name already taken (very unlikely); try another
			continue
		}
		if err := os.Rename(tmpName, dst); err != nil {
			log.Error("Unable to move media file into place: %v", err)
			return nil, errMediaWriteFailed
		}
		mf := app.newMediaFile(name, size, time.Now())
		return &mf, nil
	}
	return nil, errMediaWriteFailed
}

// handleListMedia returns all uploaded images, newest first.
func handleListMedia(app *App, u *User, w http.ResponseWriter, r *http.Request) error {
	entries, err := os.ReadDir(app.mediaPath())
	if err != nil && !os.IsNotExist(err) {
		log.Error("Unable to read media directory: %v", err)
		return ErrInternalGeneral
	}

	res := []MediaFile{}
	for _, e := range entries {
		if !mediaFilenameReg.MatchString(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		res = append(res, app.newMediaFile(e.Name(), fi.Size(), fi.ModTime()))
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].Created.After(res[j].Created)
	})
	return impart.WriteSuccess(w, res, http.StatusOK)
}
