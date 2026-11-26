cmake_minimum_required(VERSION 3.16)
project(pygorvid LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

add_library(pygorvid STATIC
    src/pygorvid.cpp
    src/h264.cpp
    src/h264_pcm.cpp
    src/h264_utils.cpp
    src/h264_encoder.cpp
    src/h264_annexb.cpp
    src/mp4_writer.cpp
    src/cabac.cpp
    src/cavlc.cpp
    src/jpeg.cpp
    src/png.cpp)
target_include_directories(pygorvid PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    $<INSTALL_INTERFACE:include>)
target_compile_options(pygorvid PRIVATE -Wall -Wextra -Wpedantic)
find_package(Threads REQUIRED)
target_link_libraries(pygorvid PUBLIC Threads::Threads)

add_executable(vidprobe src/vidprobe.cpp)
target_link_libraries(vidprobe PRIVATE pygorvid)

option(PYGORVID_SLIDESHOW "Enable SDL2 slideshow support" ON)
if(PYGORVID_SLIDESHOW)
    find_package(SDL2 REQUIRED)
    target_compile_definitions(vidprobe PRIVATE PYGORVID_SLIDESHOW)
    target_link_libraries(vidprobe PRIVATE SDL2::SDL2)
endif()

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
    set_tests_properties(pygorvid_test PROPERTIES TIMEOUT 60)
    add_executable(pygorvid_decoder_test tests/test_decoder.cpp)
    target_link_libraries(pygorvid_decoder_test PRIVATE pygorvid)
    target_compile_definitions(pygorvid_decoder_test PRIVATE
        PYGORVID_SOURCE_DIR="${CMAKE_CURRENT_SOURCE_DIR}/..")
    add_test(NAME pygorvid_decoder_test COMMAND pygorvid_decoder_test)
    set_tests_properties(pygorvid_decoder_test PROPERTIES TIMEOUT 60)
    add_test(NAME vidprobe_multislice_b_direct COMMAND ${CMAKE_COMMAND}
        -DVIDPROBE=$<TARGET_FILE:vidprobe>
        -DFIXTURE=${CMAKE_CURRENT_SOURCE_DIR}/../samples/drone2_320x240.mp4
        -DTEST_ROOT=${CMAKE_CURRENT_BINARY_DIR}
        -P ${CMAKE_CURRENT_SOURCE_DIR}/tests/test_multislice_b_direct.cmake)
    set_tests_properties(vidprobe_multislice_b_direct PROPERTIES TIMEOUT 60)
    find_program(PYGORVID_FFMPEG ffmpeg)
    if(PYGORVID_FFMPEG)
        add_test(NAME vidprobe_companion_ffmpeg COMMAND ${CMAKE_COMMAND}
            -DVIDPROBE=$<TARGET_FILE:vidprobe>
            -DFFMPEG=${PYGORVID_FFMPEG}
            -DTEST_ROOT=${CMAKE_CURRENT_BINARY_DIR}
            -P ${CMAKE_CURRENT_SOURCE_DIR}/tests/test_annexb_ffmpeg.cmake)
        set_tests_properties(vidprobe_companion_ffmpeg PROPERTIES TIMEOUT 60)
    endif()
    add_test(NAME vidprobe_image_formats COMMAND ${CMAKE_COMMAND}
        -DVIDPROBE=$<TARGET_FILE:vidprobe>
        -DFIXTURE=${CMAKE_CURRENT_SOURCE_DIR}/../samples/decoder-smoke-16x16.mp4
        -DREORDERED_FIXTURE=${CMAKE_CURRENT_SOURCE_DIR}/../testdata/h264/cavlc-32x32.mp4
        -DWEIGHTED_FIXTURE=${CMAKE_CURRENT_SOURCE_DIR}/../testdata/h264/weighted-32x32.mp4
        -DEW1_FIXTURE=${CMAKE_CURRENT_SOURCE_DIR}/../samples/ew1.mp4
        -DFFMPEG=${PYGORVID_FFMPEG}
        -DTEST_ROOT=${CMAKE_CURRENT_BINARY_DIR}
        -P ${CMAKE_CURRENT_SOURCE_DIR}/tests/test_image_formats.cmake)
    add_test(NAME vidprobe_partition_predictor_hash COMMAND ${CMAKE_COMMAND}
        -DVIDPROBE=$<TARGET_FILE:vidprobe>
        -DFIXTURE=${CMAKE_CURRENT_SOURCE_DIR}/../samples/samp3.mp4
        -DTEST_ROOT=${CMAKE_CURRENT_BINARY_DIR}
        -P ${CMAKE_CURRENT_SOURCE_DIR}/tests/test_partition_predictor.cmake)
    set_tests_properties(vidprobe_partition_predictor_hash PROPERTIES TIMEOUT 60)
    add_test(NAME vidprobe_slideshow COMMAND ${CMAKE_COMMAND}
        -DVIDPROBE=$<TARGET_FILE:vidprobe>
        -DFIXTURE=${CMAKE_CURRENT_SOURCE_DIR}/../samples/decoder-smoke-16x16.mp4
        -DENABLED=${PYGORVID_SLIDESHOW}
        -P ${CMAKE_CURRENT_SOURCE_DIR}/tests/test_slideshow.cmake)
    set_tests_properties(vidprobe_slideshow PROPERTIES TIMEOUT 60)
    add_test(NAME vidprobe_cabac_intra COMMAND ${CMAKE_COMMAND}
        -DVIDPROBE=$<TARGET_FILE:vidprobe>
        -DFIXTURE_DIR=${CMAKE_CURRENT_SOURCE_DIR}/tests/fixtures
        -DTEST_ROOT=${CMAKE_CURRENT_BINARY_DIR}
        -P ${CMAKE_CURRENT_SOURCE_DIR}/tests/test_cabac_intra.cmake)
    set_tests_properties(vidprobe_cabac_intra PROPERTIES TIMEOUT 60)
endif()

install(TARGETS pygorvid vidprobe
    ARCHIVE DESTINATION lib
    RUNTIME DESTINATION bin)
install(DIRECTORY include/ DESTINATION include)
