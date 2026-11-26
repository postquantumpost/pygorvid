---
applyTo: "**"
---

At the start of each new conversation, before answering the user, send a visible message: "Loaded (filepath)/LocalAGENTS.md".

Use the local python environment. Create it if needed with "python3 -m venv .venv". Start it
with "source .venv/bin/activate". Use "python3" instead of "/usr/bin/python3".

When working in work/pygorvid, make code changes and bug fixes in all four
languages rust, go, python, and C++, but don't run any python tests unless it can
be guaranteed they will run in less than 120 seconds. The python tests will be
run offline later.

When fixing bugs or implementing features in one language, refer to the other
language implementations if they are working.  Port implementations missing
in one language from the other languages as needed.

For the video file reader and writer objects, codecs, have the error messages show
the presentation frame number, if available, and if it applies to the operation.

The build of the four languages is controlled by the top level Makefile.
