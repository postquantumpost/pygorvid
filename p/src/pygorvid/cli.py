import sys

from .probe import probe_file


def main(argv=None) -> int:
    args = sys.argv[1:] if argv is None else argv
    if not args:
        print("usage: vidprobe FILE...", file=sys.stderr)
        return 2
    status = 0
    for path in args:
        try:
            print(f"{path}: {probe_file(path)}")
        except OSError as e:
            print(f"{path}: {e}", file=sys.stderr)
            status = 1
    return status


if __name__ == "__main__":
    sys.exit(main())
