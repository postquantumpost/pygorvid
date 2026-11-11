string(RANDOM LENGTH 16 ALPHABET 0123456789abcdef nonce)
set(directory "${TEST_ROOT}/image-formats-${nonce}")
file(MAKE_DIRECTORY "${directory}")

foreach(extension IN ITEMS png jpg JPG)
    set(output "${directory}/image with spaces.${extension}")
    execute_process(
        COMMAND "${VIDPROBE}" --input "${FIXTURE}" --extractframe 0
                --output "${output}" --prefix native_
        RESULT_VARIABLE status ERROR_VARIABLE error)
    if(NOT status EQUAL 0)
        message(FATAL_ERROR "Extraction failed: ${error}")
    endif()
    set(written "${directory}/native_image with spaces.${extension}")
    file(READ "${written}" signature HEX LIMIT 8)
    if(extension STREQUAL "png")
        if(NOT signature STREQUAL "89504e470d0a1a0a")
            message(FATAL_ERROR "PNG signature is invalid: ${signature}")
        endif()
    elseif(NOT signature MATCHES "^ffd8")
        message(FATAL_ERROR "JPEG signature is invalid: ${signature}")
    endif()
    if(NOT extension STREQUAL "png" AND FFMPEG)
        set(decoded "${directory}/decoded-${extension}.rgb")
        execute_process(
            COMMAND "${FFMPEG}" -v error -xerror -i "${written}"
                    -frames:v 1 -pix_fmt rgb24 -f rawvideo "${decoded}"
            RESULT_VARIABLE decode_status ERROR_VARIABLE decode_error)
        if(NOT decode_status EQUAL 0)
            message(FATAL_ERROR "JPEG decoding failed: ${decode_error}")
        endif()
        file(SIZE "${decoded}" decoded_size)
        if(NOT decoded_size EQUAL 768)
            message(FATAL_ERROR "Decoded JPEG is not 16x16 RGB")
        endif()
    endif()
    if(EXISTS "${output}")
        message(FATAL_ERROR "Output prefix was ignored")
    endif()
endforeach()

set(existing "${directory}/native_image with spaces.jpg")
file(READ "${existing}" original HEX)
execute_process(
    COMMAND "${VIDPROBE}" --input "${FIXTURE}" --extractframe 999
            --output "${existing}"
    RESULT_VARIABLE status ERROR_QUIET OUTPUT_QUIET)
file(READ "${existing}" after_failure HEX)
if(status EQUAL 0 OR NOT original STREQUAL after_failure)
    message(FATAL_ERROR "Failed extraction did not preserve existing JPEG")
endif()
file(REMOVE_RECURSE "${directory}")
