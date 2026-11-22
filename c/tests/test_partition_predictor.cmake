file(REMOVE_RECURSE "${TEST_ROOT}/partition-predictor")
file(MAKE_DIRECTORY "${TEST_ROOT}/partition-predictor")
execute_process(
    COMMAND "${VIDPROBE}"
        --threads 2
        --hash
        --readall
        --readahead
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
    "sha256=3860f146430490e3f239c3058802d4ed9e389901e858bb7de16e5f1e88b933ca"
    hash_position)
if(hash_position EQUAL -1)
    message(FATAL_ERROR "partition predictor hash mismatch:\n${output}")
endif()
file(REMOVE_RECURSE "${TEST_ROOT}/partition-predictor")
