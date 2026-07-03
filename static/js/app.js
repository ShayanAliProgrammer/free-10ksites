// 10K Sites Tracker — WebSocket client
// Connects once on page load, stays connected, receives push notifications.
// Only reconnects when the connection actually drops (network loss).

(function () {
    'use strict';

    var ws = null;
    var reconnectDelay = 1000;        // start at 1s
    var maxReconnectDelay = 30000;    // cap at 30s
    var reconnectTimer = null;
    var keepAliveTimer = null;
    var currentTrackingId = null;

    // Track the tracking ID currently being viewed (for targeted refresh)
    function updateCurrentTrackingId() {
        var el = document.querySelector('[data-tracking-id]');
        currentTrackingId = el ? el.getAttribute('data-tracking-id') : null;
    }

    // Handle incoming WebSocket messages
    function handleMessage(event) {
        var msg;
        try {
            msg = JSON.parse(event.data);
        } catch (e) {
            return;
        }

        if (msg.type === 'stats_updated') {
            // Refresh the stats section via HTMX (served from cache — no DB hit)
            refreshStats();
        }

        if (msg.type === 'request_updated' || msg.type === 'request_created') {
            updateCurrentTrackingId();
            // If the user is currently viewing this tracking ID, refresh the result
            if (currentTrackingId && currentTrackingId === msg.trackingId) {
                refreshTrackingResult(msg.trackingId);
            }
            // Always refresh stats (queue position may have changed)
            refreshStats();
        }

        if (msg.type === 'request_deleted') {
            updateCurrentTrackingId();
            if (currentTrackingId && currentTrackingId === msg.trackingId) {
                // The request being viewed was deleted — show search form
                refreshSearchForm();
            }
            refreshStats();
        }
    }

    function refreshStats() {
        if (typeof htmx !== 'undefined') {
            var statsEl = document.getElementById('stats-section');
            if (statsEl) {
                htmx.ajax('GET', '/stats', { target: '#stats-section', swap: 'outerHTML' });
            }
        }
    }

    function refreshTrackingResult(trackingId) {
        if (typeof htmx !== 'undefined') {
            htmx.ajax('GET', '/track/' + encodeURIComponent(trackingId), {
                target: '#track',
                swap: 'innerHTML'
            });
        }
    }

    function refreshSearchForm() {
        if (typeof htmx !== 'undefined') {
            htmx.ajax('GET', '/reset', { target: '#track', swap: 'innerHTML' });
        }
    }

    function connect() {
        var proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        var wsUrl = proto + '//' + window.location.host + '/ws';

        try {
            ws = new WebSocket(wsUrl);
        } catch (e) {
            scheduleReconnect();
            return;
        }

        ws.onopen = function () {
            reconnectDelay = 1000; // reset backoff on successful connect
            startKeepAlive();
        };

        ws.onmessage = handleMessage;

        ws.onclose = function (event) {
            stopKeepAlive();
            ws = null;
            // Only reconnect if the close wasn't intentional (code 1000 = normal closure)
            if (event.code !== 1000) {
                scheduleReconnect();
            }
        };

        ws.onerror = function () {
            // Error handler — the close handler will trigger reconnect
            if (ws) {
                try { ws.close(); } catch (e) {}
            }
        };
    }

    function scheduleReconnect() {
        if (reconnectTimer) return; // already scheduled
        reconnectTimer = setTimeout(function () {
            reconnectTimer = null;
            reconnectDelay = Math.min(reconnectDelay * 2, maxReconnectDelay);
            connect();
        }, reconnectDelay);
    }

    function startKeepAlive() {
        stopKeepAlive();
        // Send a tiny ping every 25s to keep the connection alive.
        // The server also sends pings every 30s — this is a belt-and-suspenders approach.
        keepAliveTimer = setInterval(function () {
            if (ws && ws.readyState === WebSocket.OPEN) {
                try { ws.send(JSON.stringify({ type: 'ping' })); } catch (e) {}
            }
        }, 25000);
    }

    function stopKeepAlive() {
        if (keepAliveTimer) {
            clearInterval(keepAliveTimer);
            keepAliveTimer = null;
        }
    }

    // Detect when the user comes back online after network loss
    window.addEventListener('online', function () {
        if (!ws || ws.readyState !== WebSocket.OPEN) {
            reconnectDelay = 1000; // reset backoff
            if (reconnectTimer) {
                clearTimeout(reconnectTimer);
                reconnectTimer = null;
            }
            connect();
        }
    });

    // Detect going offline — close the socket cleanly
    window.addEventListener('offline', function () {
        if (ws) {
            try { ws.close(); } catch (e) {}
        }
    });

    // Start connection on page load
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', connect);
    } else {
        connect();
    }

    // Update currentTrackingId whenever the track div updates
    if (typeof htmx !== 'undefined') {
        document.body.addEventListener('htmx:afterSwap', updateCurrentTrackingId);
    }
})();
