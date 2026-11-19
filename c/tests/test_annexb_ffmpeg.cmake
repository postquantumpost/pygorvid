file(REMOVE "${TEST_ROOT}/annexb.mp4")
file(GLOB OLD_STREAMS
    "${TEST_ROOT}/annexb-frame-*.264"
    "${TEST_ROOT}/annexb-frame-*.mp4")
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
    set(STREAM "${TEST_ROOT}/annexb-${NAME}.mp4")
    if(NOT EXISTS "${STREAM}")
        message(FATAL_ERROR "${NAME} MP4 companion was not generated")
    endif()
    execute_process(
        COMMAND "${FFMPEG}" -hide_banner -loglevel warning
            -i "${STREAM}" -map 0:v:0 -frames:v 10 -f null -
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

set(HASHES
    frame-40-422.mp4=e5a66d62ab0836c8a18a152602bce67e122882207f42e29ff820709c6816b77c
    frame-41-444.mp4=2beeee98781fb81f63716cd7ec7b162c66abdc2ab45326dabe41ea21413c2d01
    frame-41-separate.mp4=353816dc8cb9f79a11e6b00261dd367a35a68c3c51a033e350081ba3f84c4ea6
    frame-42-10bit.mp4=c4a165a1be6a507e563b5b298399804d9b71cffbceff2a54f5320221e37870df
    frame-42-12bit.mp4=fe92c204bdac8fd497687599d4e9975af13a4afaf0f6678914c93edeaa1df7ed
    frame-43-fields.mp4=74b217b854742b458826a96dd8a43764f391d27112cf836921c55a09f4140393
    frame-44-mbaff.mp4=2633954c3df2234752f72fa45bb7aec441d05eecae7e9f5b81aaea4eb0744bef
    frame-45-fmo.mp4=23071982ca210edd3798819005651a4958261d6dfb0db2ee08c377b6f62f3d23
    frame-46-redundant.mp4=dc570e9b1ed7030436e847057f2b0ab547df7ef17032f2d15324e42e01db9072
    frame-47-partitions.mp4=55d0581a7a62290402bd93c43a72ef35776c3edb7390e879db7c6e3d33cd058c
    frame-48-sp-si.mp4=cc2eca1521b71bf0b0e66ff3932731b4bcc5085c91c89082a8208a46cd47a543)
foreach(ENTRY IN LISTS HASHES)
    string(REPLACE "=" ";" FIELDS "${ENTRY}")
    list(GET FIELDS 0 NAME)
    list(GET FIELDS 1 EXPECTED)
    file(SHA256 "${TEST_ROOT}/annexb-${NAME}" ACTUAL)
    if(NOT ACTUAL STREQUAL EXPECTED)
        message(FATAL_ERROR "${NAME} SHA-256 ${ACTUAL}; expected ${EXPECTED}")
    endif()
endforeach()

file(WRITE "${TEST_ROOT}/safety.mp4" "preserve")
file(REMOVE_RECURSE "${TEST_ROOT}/safety-frame-45-fmo.mp4")
file(MAKE_DIRECTORY "${TEST_ROOT}/safety-frame-45-fmo.mp4")
execute_process(
    COMMAND "${VIDPROBE}" --generate "${TEST_ROOT}/safety.mp4"
    RESULT_VARIABLE SAFETY_STATUS
    OUTPUT_QUIET ERROR_QUIET)
if(SAFETY_STATUS EQUAL 0)
    message(FATAL_ERROR "generation accepted a directory companion destination")
endif()
file(READ "${TEST_ROOT}/safety.mp4" SAFETY_PAYLOAD)
if(NOT SAFETY_PAYLOAD STREQUAL "preserve")
    message(FATAL_ERROR "failed generation replaced the core MP4")
endif()
if(EXISTS "${TEST_ROOT}/safety-frame-40-422.mp4")
    message(FATAL_ERROR "failed generation left a partial companion")
endif()
