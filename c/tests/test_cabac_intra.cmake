set(test_dir "${TEST_ROOT}/cabac-intra-fixture-test")
file(MAKE_DIRECTORY "${test_dir}")

function(check_frames fixture)
    execute_process(
        COMMAND "${VIDPROBE}" --readall --input "${FIXTURE_DIR}/${fixture}"
        RESULT_VARIABLE result OUTPUT_VARIABLE output ERROR_VARIABLE error)
    if(NOT result EQUAL 0)
        message(FATAL_ERROR "${fixture}: readall failed: ${error}")
    endif()

    set(index 0)
    foreach(expected IN LISTS ARGN)
        set(image "${test_dir}/${index}.png")
        execute_process(
            COMMAND "${VIDPROBE}" --extractframe "${index}"
                    --output "${image}" --input "${FIXTURE_DIR}/${fixture}"
            RESULT_VARIABLE result OUTPUT_VARIABLE output ERROR_VARIABLE error)
        if(NOT result EQUAL 0)
            message(FATAL_ERROR "${fixture} frame ${index}: ${error}")
        endif()
        file(SHA256 "${image}" actual)
        if(NOT actual STREQUAL expected)
            message(FATAL_ERROR
                "${fixture} frame ${index}: expected ${expected}, got ${actual}")
        endif()
        file(REMOVE "${image}")
        math(EXPR index "${index} + 1")
    endforeach()
endfunction()

check_frames(cabac-p-intra.mp4
    2dd69a0ee3ea6df6042b6205f70b59bb7a1af6c2f6f688c48aefc61ed2344bd9
    dbf986535ddbe2b95b449939cf93bb0e83a4fc3d0652d75fff918c7f802c6666
    917fe0dc3a5614d86c2f545eee8ec700c5ccc4697d9c8d0b67ebcde6e6797055
    05e691b2aa292709fbde754e3db45b536b9fab49750e4d680ec422df44da1e33
    004f078178dd83c4d8672c679bf976b782120ab9eea36db9d098d0eb9f954c41)
check_frames(cabac-b-spatial-intra.mp4
    8be5dea3fdb5e5c362a50af97d6734ec897e633a836fe4480ffc4f92dac18033
    10204bae096ce01e276bca20de6b80a3fc2d6ccbcdac0c97f644ac2113ec3e6a
    95f73dd39f1deb025fa9db13990b1daa3b9bbacc852f65f43d02c099849a412a
    f1042eaa787be2eda2ab6147b70a94c5728f2fa59a668279eaceb4ac7c674d74
    2303afd233a0b3bd6020c373def4c42a34e8dabb4cd0dfd86d264e1e07681695
    b90e9948329ef3d221b51c9a972f6038a74398bc85bb86e16fa5888bdd53810b
    3789476b2475bfbf44aacbb50a30ebe3421b26f1b3b2af696e96b938444aa74c
    6886cba08e0aa7236284c7fef8f62817bf98d60e87a21a3f059e938a62f7a4cf)

execute_process(
    COMMAND "${VIDPROBE}" --readall --input
            "${FIXTURE_DIR}/cabac-b-temporal.mp4"
    RESULT_VARIABLE result OUTPUT_VARIABLE output ERROR_VARIABLE error)
if(result EQUAL 0 OR NOT error MATCHES "B-slice temporal direct prediction is unsupported")
    message(FATAL_ERROR "temporal-direct stream was not explicitly rejected: ${error}")
endif()
file(REMOVE_RECURSE "${test_dir}")
