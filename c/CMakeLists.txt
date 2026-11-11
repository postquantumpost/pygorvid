cmake_minimum_required(VERSION 3.16)
project(pygorvid LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

add_library(pygorvid STATIC
    src/pygorvid.cpp
    src/h264.cpp
    src/h264_pcm.cpp
    src/cabac.cpp
    src/cavlc.cpp
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
    add_executable(pygorvid_test tests/test_pygorvid.cpp)
    target_link_libraries(pygorvid_test PRIVATE pygorvid)
    target_compile_definitions(pygorvid_test PRIVATE
        PYGORVID_SOURCE_DIR="${CMAKE_CURRENT_SOURCE_DIR}/..")
    add_test(NAME pygorvid_test COMMAND pygorvid_test)
endif()

install(TARGETS pygorvid vidprobe
    ARCHIVE DESTINATION lib
    RUNTIME DESTINATION bin)
install(DIRECTORY include/ DESTINATION include)
