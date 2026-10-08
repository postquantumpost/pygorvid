import sys

from .vidfile import openfile


def main(argv=None) -> int:
    args = sys.argv[1:] if argv is None else argv
    if not args:
        print("usage: vidprobe FILE...", file=sys.stderr)
        return 2
    status = 0
    for path in args:
        with openfile(path) as v:
            if v.isopen():
                info = v.getbasicinfo()
                err = v.errorinfo()[0]
                if err:
                    print(f"{path}: {v.format}: {err}", file=sys.stderr)
                    status = 1
                else:
                    print(
                        f"{path}: {v.format} hasvideo={str(info.hasvideo).lower()}"
                        f" hasaudio={str(info.hasaudio).lower()}"
                    )
            else:
                print(f"{path}: {v.errorinfo()[0]}", file=sys.stderr)
                status = 1
    return status


if __name__ == "__main__":
    sys.exit(main())
