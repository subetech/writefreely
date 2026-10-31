/*
 * Image uploads for the editor: an "Images" modal that uploads images to
 * /api/me/media, lists uploaded images, and copies the Markdown code needed to
 * embed each one in a post.
 *
 * Requires modals.js (showModal / closeModals).
 */
(function() {
	var $modal = document.getElementById('modal-media');
	var $open = document.getElementById('open-media');
	if (!$modal || !$open) {
		return;
	}
	var $list = document.getElementById('media-list');
	var $status = document.getElementById('media-status');
	var $file = document.getElementById('media-file');
	var $drop = document.getElementById('media-drop');
	var allowedTypes = ['image/jpeg', 'image/png', 'image/gif', 'image/webp'];
	var loaded = false;
	var uploading = false;

	function setStatus(msg, isError) {
		$status.textContent = msg || '';
		$status.className = 'form-hint' + (isError ? ' error' : '');
	}

	function isOpen() {
		return $modal.style.display == 'block';
	}

	function copyText(text, cb) {
		if (navigator.clipboard && window.isSecureContext) {
			navigator.clipboard.writeText(text).then(function() { cb(true); }, function() { cb(fallbackCopy(text)); });
		} else {
			cb(fallbackCopy(text));
		}
	}

	function fallbackCopy(text) {
		var ta = document.createElement('textarea');
		ta.value = text;
		ta.setAttribute('readonly', '');
		ta.style.position = 'fixed';
		ta.style.top = '-1000px';
		document.body.appendChild(ta);
		ta.select();
		var ok = false;
		try {
			ok = document.execCommand('copy');
		} catch (e) {}
		document.body.removeChild(ta);
		return ok;
	}

	function copyButton(label, title, text) {
		var $btn = document.createElement('button');
		$btn.type = 'button';
		$btn.className = 'media-copy';
		$btn.textContent = label;
		$btn.title = title + ': ' + text;
		$btn.addEventListener('click', function(e) {
			e.preventDefault();
			copyText(text, function(ok) {
				$btn.textContent = ok ? 'Copied!' : 'Copy failed';
				$btn.classList.toggle('copied', ok);
				setTimeout(function() {
					$btn.textContent = label;
					$btn.classList.remove('copied');
				}, 1500);
			});
		});
		return $btn;
	}

	function renderItem(m, prepend) {
		var $item = document.createElement('div');
		$item.className = 'media-item';

		var $a = document.createElement('a');
		$a.href = m.url;
		$a.target = '_blank';
		$a.rel = 'noopener';
		$a.title = m.name;
		var $img = document.createElement('img');
		$img.src = m.url;
		$img.alt = '';
		$img.loading = 'lazy';
		$a.appendChild($img);
		$item.appendChild($a);

		var $actions = document.createElement('div');
		$actions.className = 'media-actions';
		$actions.appendChild(copyButton('Copy code', 'Copy the Markdown code for this image', m.markdown));
		$actions.appendChild(copyButton('Copy HTML', 'Copy the HTML code for this image', m.html));
		$item.appendChild($actions);

		if (prepend && $list.firstChild) {
			$list.insertBefore($item, $list.firstChild);
		} else {
			$list.appendChild($item);
		}
	}

	function parseErr(http) {
		try {
			return JSON.parse(http.responseText).error_msg;
		} catch (e) {
			return http.status == 413 ? 'Image is too large.' : 'Something went wrong. Please try again.';
		}
	}

	function load() {
		setStatus('Loading images...');
		var http = new XMLHttpRequest();
		http.open('GET', '/api/me/media', true);
		http.setRequestHeader('Accept', 'application/json');
		http.onreadystatechange = function() {
			if (http.readyState != 4) {
				return;
			}
			if (http.status == 200) {
				loaded = true;
				var data = JSON.parse(http.responseText).data;
				$list.innerHTML = '';
				for (var i = 0; i < data.length; i++) {
					renderItem(data[i], false);
				}
				setStatus(data.length ? '' : 'No images uploaded yet.');
			} else {
				setStatus(parseErr(http), true);
			}
		};
		http.send();
	}

	function upload(files) {
		if (uploading) {
			return;
		}
		var fd = new FormData();
		var n = 0;
		var skipped = 0;
		for (var i = 0; i < files.length; i++) {
			if (allowedTypes.indexOf(files[i].type) == -1) {
				skipped++;
				continue;
			}
			fd.append('file', files[i], files[i].name || 'image');
			n++;
		}
		if (n == 0) {
			setStatus(skipped ? 'Only JPEG, PNG, GIF and WebP images can be uploaded.' : '', true);
			return;
		}

		uploading = true;
		setStatus('Uploading ' + n + ' image' + (n == 1 ? '' : 's') + '...');
		var http = new XMLHttpRequest();
		http.open('POST', '/api/me/media', true);
		http.setRequestHeader('Accept', 'application/json');
		http.setRequestHeader('X-Requested-With', 'XMLHttpRequest');
		http.upload.onprogress = function(e) {
			if (e.lengthComputable) {
				setStatus('Uploading... ' + Math.round(e.loaded / e.total * 100) + '%');
			}
		};
		http.onreadystatechange = function() {
			if (http.readyState != 4) {
				return;
			}
			uploading = false;
			$file.value = '';
			if (http.status == 201) {
				var data = JSON.parse(http.responseText).data;
				for (var i = 0; i < data.length; i++) {
					renderItem(data[i], true);
				}
				var msg = 'Uploaded ' + data.length + ' image' + (data.length == 1 ? '' : 's') + '. Use "Copy code" or "Copy HTML" to add one to your post.';
				if (skipped) {
					msg += ' Skipped ' + skipped + ' unsupported file' + (skipped == 1 ? '' : 's') + '.';
				}
				setStatus(msg);
			} else {
				setStatus(parseErr(http), true);
			}
		};
		http.send(fd);
	}

	$open.addEventListener('click', function(e) {
		e.preventDefault();
		showModal('media');
		if (!loaded) {
			load();
		}
	});
	document.getElementById('media-close').addEventListener('click', closeModals);
	document.addEventListener('keydown', function(e) {
		if (e.key == 'Escape' && isOpen()) {
			closeModals(e);
		}
	});

	$file.addEventListener('change', function() {
		upload(this.files);
	});

	['dragenter', 'dragover'].forEach(function(ev) {
		$drop.addEventListener(ev, function(e) {
			e.preventDefault();
			$drop.classList.add('dragging');
		});
	});
	['dragleave', 'drop'].forEach(function(ev) {
		$drop.addEventListener(ev, function(e) {
			e.preventDefault();
			$drop.classList.remove('dragging');
		});
	});
	$drop.addEventListener('drop', function(e) {
		if (e.dataTransfer && e.dataTransfer.files.length) {
			upload(e.dataTransfer.files);
		}
	});

	// Paste images from the clipboard while the modal is open
	document.addEventListener('paste', function(e) {
		if (!isOpen() || !e.clipboardData || !e.clipboardData.files.length) {
			return;
		}
		e.preventDefault();
		upload(e.clipboardData.files);
	});
})();
