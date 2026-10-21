"""Serve a directory and redirect matching paths, like Cloudflare _redirects.

Usage: redirect_server.py ROOT PORT CODE PREFIX=TARGET [PREFIX=TARGET ...]

A request whose path starts with PREFIX is redirected to TARGET plus the rest
of the path (a splat). Everything else is served from ROOT. Every request is
logged so the probe can show what the package manager actually fetched.
"""

import http.server
import sys
from functools import partial

root, port, code = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])
rules = [arg.split("=", 1) for arg in sys.argv[4:]]


class Handler(http.server.SimpleHTTPRequestHandler):
    def _redirect(self):
        for prefix, target in rules:
            if self.path.startswith(prefix):
                location = target + self.path[len(prefix):]
                self.send_response(code)
                self.send_header("Location", location)
                self.send_header("Content-Length", "0")
                self.end_headers()
                print(f"REDIRECT {code} {self.path} -> {location}", flush=True)
                return True
        return False

    def do_GET(self):
        if not self._redirect():
            print(f"SERVE {self.path}", flush=True)
            super().do_GET()

    def do_HEAD(self):
        if not self._redirect():
            super().do_HEAD()

    def log_message(self, *args):
        pass


http.server.ThreadingHTTPServer(("127.0.0.1", port), partial(Handler, directory=root)).serve_forever()
