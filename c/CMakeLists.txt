cmake_minimum_required(VERSION 3.16)
project(pygorvid LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

add_library(pygorvid STATIC
    src/pygorvid.cpp
    src/h264.cpp
    src/h264_pcm.cpp
    src/h264_encoder.cpp
    src/mp4_writer.cpp
    src/cabac.cpp
    src/cavlc.cpp
    src/jpeg.cpp
    src/png.cpp)
target_include_directories(pygorvid PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    $<INSTALL_INTERFACE:include>)
target_compile_options(pygorvid PRIVATE -Wall -Wextra -Wpedantic)

add_executable(vidprobe src/vidprobe.cpp)
target_link_libraries(vidprobe PRIVATE pygorvid)

option(PYGORVID_BUILD_TESTS "Build the pygorvid test suite" OFF)
if(PYGORVID_BUILD_TESTS)
    enable_testing()
    add_executable(pygorvid_generate_test tests/test_generate.cpp)
    target_link_libraries(pygorvid_generate_test PRIVATE pygorvid)
    add_test(NAME pygorvid_generate_test COMMAND pygorvid_generate_test)
    set_tests_properties(pygorvid_generate_test PROPERTIES TIMEOUT 60)
    add_executable(pygorvid_test tests/test_pygorvid.cpp)
    target_link_libraries(pygorvid_test PRIVATE pygorvid)
    target_compile_definitions(pygorvid_test PRIVATE
        PYGORVID_SOURCE_DIR="${CMAKE_CURRENT_SOURCE_DIR}/..")
    add_test(NAME pygorvid_test COMMAND pygorvid_test)
    find_program(PYGORVID_FFMPEG ffmpeg)
    add_test(NAME vidprobe_image_formats COMMAND ${CMAKE_COMMAND}
        -DVIDPROBE=$<TARGET_FILE:vidprobe>
        -DFIXTURE=${CMAKE_CURRENT_SOURCE_DIR}/../samples/decoder-smoke-16x16.mp4
        -DFFMPEG=${PYGORVID_FFMPEG}
        -DTEST_ROOT=${CMAKE_CURRENT_BINARY_DIR}
        -P ${CMAKE_CURRENT_SOURCE_DIR}/tests/test_image_formats.cmake)
endif()

install(TARGETS pygorvid vidprobe
    ARCHIVE DESTINATION lib
    RUNTIME DESTINATION bin)
install(DIRECTORY include/ DESTINATION include)
