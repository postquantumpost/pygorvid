file(REMOVE "${TEST_ROOT}/annexb.mp4")
file(GLOB OLD_STREAMS "${TEST_ROOT}/annexb-frame-*.264")
if(OLD_STREAMS)
    file(REMOVE ${OLD_STREAMS})
endif()
execute_process(
    COMMAND "${VIDPROBE}" --generate "${TEST_ROOT}/annexb.mp4"
    RESULT_VARIABLE GENERATE_STATUS
    OUTPUT_QUIET ERROR_VARIABLE GENERATE_ERROR)
if(NOT GENERATE_STATUS EQUAL 0)
    message(FATAL_ERROR "generation failed: ${GENERATE_ERROR}")
endif()
foreach(NAME
        frame-40-422 frame-41-444 frame-41-separate
        frame-42-10bit frame-42-12bit frame-43-fields frame-44-mbaff
        frame-45-fmo frame-46-redundant frame-47-partitions frame-48-sp-si)
    set(STREAM "${TEST_ROOT}/annexb-${NAME}.264")
    execute_process(
        COMMAND "${FFMPEG}" -hide_banner -loglevel warning
            -f h264 -i "${STREAM}" -frames:v 10 -f null -
        RESULT_VARIABLE FFMPEG_STATUS
        OUTPUT_QUIET ERROR_VARIABLE FFMPEG_ERROR)
    if(FFMPEG_STATUS EQUAL 0)
        continue()
    endif()
    if(NAME STREQUAL "frame-41-separate" AND
       FFMPEG_ERROR MATCHES "separate color planes are not supported")
        continue()
    endif()
    if(NAME STREQUAL "frame-45-fmo" AND
       FFMPEG_ERROR MATCHES "FMO is not implemented")
        continue()
    endif()
    message(FATAL_ERROR "${NAME} failed ffmpeg validation:\n${FFMPEG_ERROR}")
endforeach()
