---
applyTo: "**"
---

At the start of each new conversation, before answering the user, send a visible message: "Loaded (filepath)/LocalAGENTS.md".

Use the local python environment. Create it if needed with "python3 -m venv .venv". Start it
with "source .venv/bin/activate". Use "python3" instead of "/usr/bin/python3".

When working in work/pygorvid, make code changes and bug fixes in all three
languages rust, go, and python, but don't run any python tests unless it can
be guaranteed they will run in less than 60 seconds. The python tests will be
run offline later.
