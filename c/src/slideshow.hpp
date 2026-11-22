#pragma once

#include "pygorvid/h264.hpp"
#ifndef SDL_MAIN_HANDLED
#define SDL_MAIN_HANDLED
#endif
#include <SDL.h>
#include <algorithm>
#include <chrono>
#include <limits>
#include <memory>
#include <stdexcept>
#include <string>
#include <utility>

class Slideshow {
public:
    explicit Slideshow(std::string title) : title_(std::move(title)) {
        SDL_SetMainReady();
        if (SDL_InitSubSystem(SDL_INIT_VIDEO) != 0) fail("initialize SDL2 video");
        initialized_ = true;
    }

    ~Slideshow() {
        texture_.reset();
        renderer_.reset();
        window_.reset();
        if (initialized_) SDL_QuitSubSystem(SDL_INIT_VIDEO);
    }

    bool wait(std::chrono::steady_clock::time_point deadline) {
        for (;;) {
            const auto remaining = deadline - std::chrono::steady_clock::now();
            if (remaining <= std::chrono::steady_clock::duration::zero()) {
                return true;
            }
            const auto timeout = std::chrono::duration_cast<std::chrono::milliseconds>(
                remaining + std::chrono::milliseconds(1)).count();
            const auto clamped_timeout = std::clamp<std::int64_t>(
                timeout, 1, std::numeric_limits<int>::max());
            SDL_Event event;
            if (SDL_WaitEventTimeout(&event, static_cast<int>(clamped_timeout)) == 1) {
                if (event.type == SDL_QUIT ||
                    (event.type == SDL_WINDOWEVENT && event.window.event == SDL_WINDOWEVENT_CLOSE))
                    return false;
            } else if (std::chrono::steady_clock::now() >= deadline) {
                return true;
            }
        }
    }

    void show(const pygorvid::Yuv420Frame& frame) {
        if (!window_) {
            const auto scale = std::min({
                1.0,
                1024.0 / static_cast<double>(frame.width),
                768.0 / static_cast<double>(frame.height),
            });
            const auto window_width = std::max(
                1, static_cast<int>(frame.width * scale));
            const auto window_height = std::max(
                1, static_cast<int>(frame.height * scale));
            window_.reset(SDL_CreateWindow(title_.c_str(), SDL_WINDOWPOS_CENTERED, SDL_WINDOWPOS_CENTERED,
                window_width, window_height, SDL_WINDOW_SHOWN | SDL_WINDOW_RESIZABLE));
            if (!window_) fail("create slideshow window");
            renderer_.reset(SDL_CreateRenderer(window_.get(), -1, 0));
            if (!renderer_) fail("create slideshow renderer");
        }
        if (!texture_ || width_ != frame.width || height_ != frame.height) {
            texture_.reset(SDL_CreateTexture(renderer_.get(), SDL_PIXELFORMAT_IYUV,
                SDL_TEXTUREACCESS_STREAMING, static_cast<int>(frame.width), static_cast<int>(frame.height)));
            if (!texture_) fail("create slideshow texture");
            width_ = frame.width;
            height_ = frame.height;
        }
        const auto* u = frame.chroma_u.data();
        const auto* v = frame.chroma_v.data();
        std::vector<std::uint8_t> display_u;
        std::vector<std::uint8_t> display_v;
        if (frame.chroma_u.size() >= static_cast<std::size_t>(frame.width) * frame.height ||
            frame.chroma_u.size() >= static_cast<std::size_t>(frame.width) * frame.height / 2) {
            const auto source_width =
                frame.chroma_u.size() >= static_cast<std::size_t>(frame.width) * frame.height
                    ? frame.width
                    : frame.width / 2;
            const auto source_height = frame.height;
            const auto pitch = (frame.width + 1) / 2;
            display_u.resize(static_cast<std::size_t>(pitch) * ((frame.height + 1) / 2));
            display_v.resize(display_u.size());
            for (std::uint32_t row = 0; row < (frame.height + 1) / 2; ++row) {
                for (std::uint32_t column = 0; column < pitch; ++column) {
                    const auto sx = std::min(column * 2, frame.width - 1);
                    const auto sy0 = std::min(row * 2, frame.height - 1);
                    const auto sy1 = std::min(row * 2 + 1, frame.height - 1);
                    const auto y_scale =
                        source_width == frame.width ? 1U : 0U;
                    (void)source_height;
                    const auto read = [&](const auto* plane) {
                        if (source_width == frame.width) {
                            return (static_cast<std::uint32_t>(plane[sy0 * source_width + sx]) +
                                    plane[sy0 * source_width + std::min(sx + 1, frame.width - 1)] +
                                    plane[sy1 * source_width + sx] +
                                    plane[sy1 * source_width + std::min(sx + 1, frame.width - 1)] + 2) / 4;
                        }
                        return (static_cast<std::uint32_t>(plane[sy0 * source_width + column]) +
                                plane[sy1 * source_width + column] + 1) / 2;
                    };
                    display_u[row * pitch + column] = static_cast<std::uint8_t>(read(u));
                    display_v[row * pitch + column] = static_cast<std::uint8_t>(read(v));
                    (void)y_scale;
                }
            }
            u = display_u.data();
            v = display_v.data();
        }
        if (SDL_UpdateYUVTexture(texture_.get(), nullptr, frame.luma.data(), frame.width,
                u, (frame.width + 1) / 2, v, (frame.width + 1) / 2) != 0) fail("update slideshow texture");
        if (SDL_RenderClear(renderer_.get()) != 0) fail("clear slideshow window");
        if (SDL_RenderCopy(renderer_.get(), texture_.get(), nullptr, nullptr) != 0) fail("render slideshow frame");
        SDL_RenderPresent(renderer_.get());
    }

private:
    [[noreturn]] static void fail(const char* operation) {
        throw std::runtime_error(std::string(operation) + ": " + SDL_GetError());
    }
    std::string title_;
    bool initialized_ = false;
    std::uint32_t width_ = 0, height_ = 0;
    std::unique_ptr<SDL_Window, decltype(&SDL_DestroyWindow)> window_{nullptr, SDL_DestroyWindow};
    std::unique_ptr<SDL_Renderer, decltype(&SDL_DestroyRenderer)> renderer_{nullptr, SDL_DestroyRenderer};
    std::unique_ptr<SDL_Texture, decltype(&SDL_DestroyTexture)> texture_{nullptr, SDL_DestroyTexture};
};
