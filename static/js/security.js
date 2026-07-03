// 10K Sites Tracker — DevTools deterrent
//
// IMPORTANT: Client-side DevTools blocking is NOT a security boundary.
// A determined user can always bypass these measures by disabling JS,
// using curl, or launching the browser with DevTools already open.
//
// The REAL security is server-side:
//   - CSP headers prevent XSS
//   - HttpOnly cookies prevent session theft via JS
//   - Parameterized queries prevent SQL injection
//   - Rate limiting prevents brute force
//   - Template auto-escaping prevents HTML injection
//
// This script raises the bar for casual inspection but should not be
// relied upon as a security control.

(function () {
    'use strict';

    // 1. Block keyboard shortcuts for DevTools
    document.addEventListener('keydown', function (e) {
        // F12
        if (e.keyCode === 123) {
            e.preventDefault();
            e.stopPropagation();
            return false;
        }
        // Ctrl+Shift+I (Inspect), Ctrl+Shift+J (Console), Ctrl+Shift+C (Inspect Element)
        if (e.ctrlKey && e.shiftKey && (e.keyCode === 73 || e.keyCode === 74 || e.keyCode === 67)) {
            e.preventDefault();
            e.stopPropagation();
            return false;
        }
        // Ctrl+U (View Source)
        if (e.ctrlKey && e.keyCode === 85) {
            e.preventDefault();
            e.stopPropagation();
            return false;
        }
        // Ctrl+Shift+K (Firefox console)
        if (e.ctrlKey && e.shiftKey && e.keyCode === 75) {
            e.preventDefault();
            e.stopPropagation();
            return false;
        }
        // Cmd+Opt+I/J/C (Mac)
        if (e.metaKey && e.altKey && (e.keyCode === 73 || e.keyCode === 74 || e.keyCode === 67)) {
            e.preventDefault();
            e.stopPropagation();
            return false;
        }
    }, true); // capture phase to intercept before page handlers

    // 2. Block right-click context menu
    document.addEventListener('contextmenu', function (e) {
        e.preventDefault();
        e.stopPropagation();
        return false;
    }, true);

    // 3. Block selection (makes it harder to inspect text)
    // Note: this is aggressive — remove if it hurts UX
    // document.addEventListener('selectstart', function(e) { e.preventDefault(); return false; }, true);

    // 4. Detect DevTools via window dimension difference
    // When DevTools is docked, the outer window is significantly larger than inner
    var threshold = 160;
    function checkDevToolsSize() {
        var widthDiff = window.outerWidth - window.innerWidth;
        var heightDiff = window.outerHeight - window.innerHeight;
        if (widthDiff > threshold || heightDiff > threshold) {
            devtoolsDetected();
        }
    }

    // 5. Detect DevTools via debugger statement timing
    // The debugger statement pauses execution when DevTools is open
    function checkDevToolsDebugger() {
        var start = performance.now();
        // eslint-disable-next-line no-debugger
        debugger;
        var end = performance.now();
        if (end - start > 100) {
            devtoolsDetected();
        }
    }

    // 6. Detect DevTools via console.log timing (older trick, still works in some browsers)
    var devtoolsOpen = false;
    var element = new Image();
    Object.defineProperty(element, 'id', {
        get: function () {
            devtoolsOpen = true;
            // Don't clear the page — just silently flag
        }
    });

    function devtoolsDetected() {
        // Instead of disrupting the page, we just clear the console
        // Disrupting the page would harm UX for legitimate users
        try { console.clear(); } catch (e) {}
    }

    // 7. Override console methods to no-op (prevents console-based inspection)
    // This makes console.log/error/warn/info non-functional
    try {
        console.log = function () {};
        console.info = function () {};
        console.warn = function () {};
        console.error = function () {};
        console.debug = function () {};
        console.table = function () {};
        console.dir = function () {};
        console.trace = function () {};
        console.group = function () {};
        console.groupEnd = function () {};
        console.profile = function () {};
        console.profileEnd = function () {};
        console.time = function () {};
        console.timeEnd = function () {};
        console.assert = function () {};
        console.count = function () {};
    } catch (e) {}

    // 8. Periodically clear console
    setInterval(function () {
        try { console.clear(); } catch (e) {}
        checkDevToolsSize();
    }, 2000);

    // 9. Run debugger check periodically (less frequent — it's heavier)
    setInterval(function () {
        checkDevToolsDebugger();
    }, 5000);

    // 10. Block drag (preffects dragging elements to inspect)
    document.addEventListener('dragstart', function (e) {
        e.preventDefault();
        return false;
    }, true);

    // 11. Prevent saving the page (Ctrl+S)
    document.addEventListener('keydown', function (e) {
        if (e.ctrlKey && e.keyCode === 83) { // Ctrl+S
            e.preventDefault();
            e.stopPropagation();
            return false;
        }
    }, true);

    // 12. Detect if DevTools was open before page load (some browsers launch with it open)
    // Check via toString() trick on console methods
    (function checkInitialDevTools() {
        var start = performance.now();
        // eslint-disable-next-line no-debugger
        debugger;
        if (performance.now() - start > 100) {
            devtoolsDetected();
        }
    })();
})();
