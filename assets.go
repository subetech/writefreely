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
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/writefreely/writefreely/config"
)

// Static files are served with a long-lived Cache-Control header (see
// cacheControl), so templates reference them through the `asset` template
// function, which appends a version based on the file's contents, e.g.
// /css/write.css?v=3f2a9c01be. When a file changes, its URL changes, and
// browsers fetch the new version; unchanged files stay cached.
//
// Versions are computed once per file and kept until the app restarts, which
// happens on every deploy.

var (
	assetsStaticDir string
	assetVersions   = &sync.Map{} // URL path -> version
)

func initAssets(cfg *config.Config) {
	assetsStaticDir = filepath.Join(cfg.Server.StaticParentDir, staticDir)
	assetVersions = &sync.Map{}
}

// assetURL returns the given static file path (e.g. "/css/write.css") with a
// version query parameter derived from the file's contents. If the file can't
// be read, the path is returned as-is.
func assetURL(p string) string {
	if v, ok := assetVersions.Load(p); ok {
		return p + "?v=" + v.(string)
	}

	clean := path.Clean("/" + p)
	if clean != p || strings.Contains(p, "..") {
		return p
	}
	data, err := os.ReadFile(filepath.Join(assetsStaticDir, filepath.FromSlash(clean)))
	if err != nil {
		// Don't cache misses (e.g. an optional custom.css), so a file added
		// later still gets versioned.
		return p
	}
	sum := sha256.Sum256(data)
	v := hex.EncodeToString(sum[:5])
	assetVersions.Store(p, v)
	return p + "?v=" + v
}
