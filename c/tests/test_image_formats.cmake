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

set(hash_output_file "${directory}/hashed.png")
execute_process(
    COMMAND "${VIDPROBE}" --hash --extractframe 1 --input "${FIXTURE}"
            --output "${hash_output_file}"
    RESULT_VARIABLE status OUTPUT_VARIABLE hash_output ERROR_VARIABLE error)
if(NOT status EQUAL 0 OR NOT EXISTS "${hash_output_file}")
    message(FATAL_ERROR "Hash extraction failed: ${error}${hash_output}")
endif()
if(NOT hash_output MATCHES
       "frame 1 sha256=b9d622c5a46a873024f92f79adc3d2eff56b28361826a5cfbfaa2d99c47e0fc0")
    message(FATAL_ERROR "Hash extraction produced an unexpected frame digest: ${hash_output}")
endif()

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

set(generated "${directory}/readall.mp4")
execute_process(
    COMMAND "${VIDPROBE}" --generate "${generated}"
    RESULT_VARIABLE status ERROR_VARIABLE error)
if(NOT status EQUAL 0)
    message(FATAL_ERROR "Generation for readall test failed: ${error}")
endif()
execute_process(
    COMMAND "${VIDPROBE}" --prefix jc_ --readall --input "${generated}"
    RESULT_VARIABLE status OUTPUT_VARIABLE output ERROR_VARIABLE error)
if(NOT status EQUAL 0 OR NOT output MATCHES "read 60 frames")
    message(FATAL_ERROR "Readall failed: ${error}${output}")
endif()

execute_process(
    COMMAND "${VIDPROBE}" --readall --hash --input "${FIXTURE}"
    RESULT_VARIABLE status OUTPUT_VARIABLE hash_output ERROR_VARIABLE error)
if(NOT status EQUAL 0)
    message(FATAL_ERROR "--readall --hash failed: ${error}${hash_output}")
endif()
if(NOT hash_output MATCHES
       "frame 0 sha256=ab53e317c9229d1721018ffc000d6e4ecadb43706b38544b197ef7e8c9299644")
    message(FATAL_ERROR "--readall --hash produced an unexpected frame digest: ${hash_output}")
endif()

execute_process(
    COMMAND "${VIDPROBE}" --readall --hash --input "${REORDERED_FIXTURE}"
    RESULT_VARIABLE status OUTPUT_VARIABLE reordered_hash_output
    ERROR_VARIABLE error)
if(NOT status EQUAL 0)
    message(FATAL_ERROR "Reordered --readall --hash failed: ${error}${reordered_hash_output}")
endif()
string(FIND "${reordered_hash_output}"
    "frame 1 sha256=97a6ee33c31b09a278367a7a59e55ce7ea9b48d2203c28dbe803317b8b1a0a95"
    frame_one_position)
string(FIND "${reordered_hash_output}"
    "frame 2 sha256=8e0a9935bf908f0a17def7e8c7bc103ac9937022bb48342f956d984e436e8ced"
    frame_two_position)
if(frame_one_position EQUAL -1 OR frame_two_position EQUAL -1 OR
   NOT frame_one_position LESS frame_two_position)
    message(FATAL_ERROR "Reordered hash output is not in presentation order: ${reordered_hash_output}")
endif()

execute_process(
    COMMAND "${VIDPROBE}" --readall --hash --input "${WEIGHTED_FIXTURE}"
    RESULT_VARIABLE status OUTPUT_VARIABLE weighted_hash_output
    ERROR_VARIABLE error)
if(NOT status EQUAL 0 OR NOT weighted_hash_output MATCHES
       "frame 1 sha256=49dd89111184d6cc2225abf9ab71de58edf8bc39f8e2d67ecafd19584acf332e")
    message(FATAL_ERROR "Weighted hash output is inconsistent: ${error}${weighted_hash_output}")
endif()

execute_process(
    COMMAND "${VIDPROBE}" --extractframe 7 --hash --input "${EW1_FIXTURE}"
            --output "${directory}/ew1-frame7.png"
    RESULT_VARIABLE status OUTPUT_VARIABLE ew1_hash_output
    ERROR_VARIABLE error)
if(NOT status EQUAL 0 OR NOT ew1_hash_output MATCHES
       "frame 7 sha256=d9328bd65b2f378c0acedc532f5d1ccea980b7c776a21db5c4e42c5233727f33")
    message(FATAL_ERROR "Co-located motion hash output is inconsistent: ${error}${ew1_hash_output}")
endif()

execute_process(
    COMMAND "${VIDPROBE}" --readall --extractframe 0 --output "${existing}"
            "${generated}"
    RESULT_VARIABLE status OUTPUT_QUIET ERROR_QUIET)
if(status EQUAL 0)
    message(FATAL_ERROR "--readall accepted incompatible extraction options")
endif()
file(REMOVE_RECURSE "${directory}")
