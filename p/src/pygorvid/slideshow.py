"""SDL2 display for decoded frames; SDL2 is loaded only for slideshow mode."""

import ctypes
import importlib.util
import time

from .colorconv import PixelConverter
from .pixelbuffer import PixelColorRange


class SlideshowError(RuntimeError):
    """The slideshow window could not be opened or updated."""


def _load_sdl():
    if importlib.util.find_spec("sdl2") is None:
        raise SlideshowError("optional slideshow feature was not included; install pygorvid[slideshow]")
    try:
        import sdl2
    except (ImportError, RuntimeError, OSError) as error:
        raise SlideshowError(f"cannot load SDL2 runtime: {error}") from error
    return sdl2


class SlideshowWindow:
    def __init__(self, title: str):
        self._sdl = _load_sdl()
        self._title = title.encode("utf-8")
        self._window = None
        self._renderer = None
        self._texture = None
        self._dimensions = None
        self._initialized = False
        self._converter = PixelConverter(PixelColorRange.LIMITED)
        if self._sdl.SDL_InitSubSystem(0x20) != 0:
            raise self._error("initialize SDL2 video")
        self._initialized = True

    def _error(self, operation: str) -> SlideshowError:
        message = self._sdl.SDL_GetError()
        detail = message.decode("utf-8", errors="replace") if message else "unknown SDL2 error"
        return SlideshowError(f"{operation}: {detail}")

    def close(self):
        if self._texture:
            self._sdl.SDL_DestroyTexture(self._texture)
            self._texture = None
        if self._renderer:
            self._sdl.SDL_DestroyRenderer(self._renderer)
            self._renderer = None
        if self._window:
            self._sdl.SDL_DestroyWindow(self._window)
            self._window = None
        if self._initialized:
            self._sdl.SDL_QuitSubSystem(0x20)
            self._initialized = False

    def is_open(self) -> bool:
        event = self._sdl.SDL_Event()
        while self._sdl.SDL_PollEvent(ctypes.byref(event)):
            if event.type == self._sdl.SDL_QUIT or (
                event.type == self._sdl.SDL_WINDOWEVENT
                and event.window.event == self._sdl.SDL_WINDOWEVENT_CLOSE
            ):
                return False
        return True

    def wait_until(self, deadline_seconds: float) -> bool:
        while self.is_open():
            remaining_seconds = deadline_seconds - time.monotonic()
            if remaining_seconds <= 0:
                return True
            time.sleep(min(remaining_seconds, 0.02))
        return False

    def show(self, frame) -> None:
        rgb = self._converter.convert_yuv420_to_rgb(
            frame.width, frame.height, frame.y, frame.u, frame.v,
            frame.y_stride, frame.u_stride, frame.v_stride,
        )
        if not self._window:
            centered = self._sdl.SDL_WINDOWPOS_CENTERED
            self._window = self._sdl.SDL_CreateWindow(
                self._title, centered, centered, rgb.width, rgb.height,
                self._sdl.SDL_WINDOW_SHOWN | self._sdl.SDL_WINDOW_RESIZABLE,
            )
            if not self._window:
                raise self._error("create slideshow window")
            self._renderer = self._sdl.SDL_CreateRenderer(self._window, -1, 0)
            if not self._renderer:
                raise self._error("create slideshow renderer")
        if self._dimensions != (rgb.width, rgb.height):
            if self._texture:
                self._sdl.SDL_DestroyTexture(self._texture)
                self._texture = None
            self._texture = self._sdl.SDL_CreateTexture(
                self._renderer, self._sdl.SDL_PIXELFORMAT_RGB24,
                self._sdl.SDL_TEXTUREACCESS_STREAMING, rgb.width, rgb.height,
            )
            if not self._texture:
                raise self._error("create slideshow texture")
            self._dimensions = (rgb.width, rgb.height)
        pixels = ctypes.create_string_buffer(rgb.data)
        if self._sdl.SDL_UpdateTexture(self._texture, None, pixels, rgb.stride) != 0:
            raise self._error("update slideshow texture")
        if self._sdl.SDL_RenderClear(self._renderer) != 0:
            raise self._error("clear slideshow window")
        if self._sdl.SDL_RenderCopy(self._renderer, self._texture, None, None) != 0:
            raise self._error("render slideshow frame")
        self._sdl.SDL_RenderPresent(self._renderer)
