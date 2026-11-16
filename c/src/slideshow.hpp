#pragma once

#include "pygorvid/h264.hpp"
#ifndef SDL_MAIN_HANDLED
#define SDL_MAIN_HANDLED
#endif
#include <SDL.h>
#include <algorithm>
#include <chrono>
#include <memory>
#include <stdexcept>
#include <string>
#include <thread>
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
            SDL_Event event;
            while (SDL_PollEvent(&event)) {
                if (event.type == SDL_QUIT ||
                    (event.type == SDL_WINDOWEVENT && event.window.event == SDL_WINDOWEVENT_CLOSE))
                    return false;
            }
            const auto now = std::chrono::steady_clock::now();
            if (now >= deadline) return true;
            std::this_thread::sleep_for(std::min(deadline - now,
                std::chrono::steady_clock::duration(std::chrono::milliseconds(20))));
        }
    }

    void show(const pygorvid::Yuv420Frame& frame) {
        if (!window_) {
            window_.reset(SDL_CreateWindow(title_.c_str(), SDL_WINDOWPOS_CENTERED, SDL_WINDOWPOS_CENTERED,
                static_cast<int>(frame.width), static_cast<int>(frame.height), SDL_WINDOW_SHOWN | SDL_WINDOW_RESIZABLE));
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
        if (SDL_UpdateYUVTexture(texture_.get(), nullptr, frame.luma.data(), frame.width,
                frame.chroma_u.data(), (frame.width + 1) / 2,
                frame.chroma_v.data(), (frame.width + 1) / 2) != 0) fail("update slideshow texture");
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
