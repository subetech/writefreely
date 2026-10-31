/*
 * Helpers for scheduled posts: showing publish times in the viewer's own time
 * zone (and saying which one it is).
 *
 * On load, this rewrites every <time class="scheduled-at" datetime="..."> to
 * the local date and time, followed by the time zone, and fills every
 * .tz-label element with the current time zone.
 */
var WFTime = (function() {
	var pad = function(n) {
		return ('0' + n).slice(-2);
	};

	// Describes the viewer's time zone at the given date (offsets change with
	// daylight saving time), e.g. "Europe/Berlin, UTC+1".
	var tzLabel = function(d) {
		d = d && !isNaN(d.getTime()) ? d : new Date();
		var off = -d.getTimezoneOffset();
		var sign = off >= 0 ? '+' : '−';
		off = Math.abs(off);
		var s = 'UTC' + sign + Math.floor(off / 60) + (off % 60 ? ':' + pad(off % 60) : '');
		var name = '';
		try {
			name = Intl.DateTimeFormat().resolvedOptions().timeZone || '';
		} catch (e) {}
		return name ? name + ', ' + s : s;
	};

	// Formats a Date as a value for <input type="datetime-local">, in local time.
	var toLocalInput = function(d) {
		return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) + 'T' + pad(d.getHours()) + ':' + pad(d.getMinutes());
	};

	// Formats a Date for the API, in UTC (e.g. "2026-11-01T08:00:00Z").
	var toAPI = function(d) {
		return d.toISOString().slice(0, 19) + 'Z';
	};

	var format = function(d) {
		var lang = document.documentElement.lang || navigator.language || 'en-US';
		try {
			return d.toLocaleString(lang, {year: 'numeric', month: 'long', day: 'numeric', hour: '2-digit', minute: '2-digit'});
		} catch (e) {
			return d.toLocaleString();
		}
	};

	var localizeAll = function() {
		var times = document.querySelectorAll('time.scheduled-at');
		for (var i = 0; i < times.length; i++) {
			var d = new Date(times[i].getAttribute('datetime'));
			if (isNaN(d.getTime())) {
				continue;
			}
			times[i].title = times[i].textContent;
			times[i].textContent = format(d) + ' (' + tzLabel(d) + ')';
		}
		var labels = document.querySelectorAll('.tz-label');
		for (i = 0; i < labels.length; i++) {
			labels[i].textContent = tzLabel(new Date());
		}
	};

	if (document.readyState === 'loading') {
		document.addEventListener('DOMContentLoaded', localizeAll);
	} else {
		localizeAll();
	}

	return {
		tzLabel: tzLabel,
		toLocalInput: toLocalInput,
		toAPI: toAPI,
		format: format,
		localizeAll: localizeAll
	};
})();
