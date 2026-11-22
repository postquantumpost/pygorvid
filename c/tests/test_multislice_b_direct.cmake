file(REMOVE_RECURSE "${TEST_ROOT}/multislice-b-direct")
file(MAKE_DIRECTORY "${TEST_ROOT}/multislice-b-direct")
execute_process(
    COMMAND "${VIDPROBE}"
        --hash
        --extractframe 5
        --output "${TEST_ROOT}/multislice-b-direct/frame.png"
        --input "${FIXTURE}"
    RESULT_VARIABLE result
    OUTPUT_VARIABLE output
    ERROR_VARIABLE error
    OUTPUT_STRIP_TRAILING_WHITESPACE)
if(NOT result EQUAL 0)
    message(FATAL_ERROR "vidprobe failed: ${error}")
endif()
string(FIND
    "${output}"
    "sha256=23e98052eb9e626bf300321b1f7764142d2f3ae656deb8611597441df458767d"
    hash_position)
if(hash_position EQUAL -1)
    message(FATAL_ERROR "multi-slice B-direct hash mismatch:\n${output}")
endif()
file(REMOVE_RECURSE "${TEST_ROOT}/multislice-b-direct")
