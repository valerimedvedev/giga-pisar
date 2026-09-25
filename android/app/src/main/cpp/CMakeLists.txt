cmake_minimum_required(VERSION 3.22.1)
project(pisar_llm LANGUAGES C CXX)

set(CMAKE_C_STANDARD 11)
set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# llama.cpp — из GitHub, закреплённый выпуск (один и тот же у всех сборок).
# Свой путь: -DLLAMA_DIR=/путь/к/llama.cpp (например, уже скачанный).
set(LLAMA_TAG "v0.5.0" CACHE STRING "выпуск llama.cpp")
if(DEFINED LLAMA_DIR AND EXISTS "${LLAMA_DIR}/CMakeLists.txt")
    set(llama_SOURCE_DIR "${LLAMA_DIR}")
else()
    include(FetchContent)
    FetchContent_Declare(llama
        GIT_REPOSITORY https://github.com/ggml-org/llama.cpp
        GIT_TAG        ${LLAMA_TAG}
        GIT_SHALLOW    TRUE
        GIT_PROGRESS   TRUE)
    FetchContent_GetProperties(llama)
    if(NOT llama_POPULATED)
        FetchContent_Populate(llama)
    endif()
endif()

# Одна статическая сборка под arm64 телефона: без динамических бэкендов,
# с инструкциями dotprod/i8mm (Snapdragon 8 Gen 2 их умеет) и KleidiAI.
set(BUILD_SHARED_LIBS OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_TESTS OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_EXAMPLES OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_TOOLS OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_SERVER OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_COMMON ON CACHE BOOL "" FORCE)
set(LLAMA_OPENSSL OFF CACHE BOOL "" FORCE)
set(GGML_NATIVE OFF CACHE BOOL "" FORCE)
set(GGML_BACKEND_DL OFF CACHE BOOL "" FORCE)
set(GGML_LLAMAFILE OFF CACHE BOOL "" FORCE)
set(GGML_OPENMP OFF CACHE BOOL "" FORCE)
if(ANDROID_ABI STREQUAL "arm64-v8a")
    set(GGML_CPU_ARM_ARCH "armv8.2-a+dotprod+i8mm+fp16" CACHE STRING "" FORCE)
    # KleidiAI (ядра Arm для int8) даёт ещё +20–40 % на S23 Ultra, но тянет исходники
    # с gitlab.arm.com при сборке; включить: -DGGML_CPU_KLEIDIAI=ON
    set(GGML_CPU_KLEIDIAI OFF CACHE BOOL "")
endif()

add_subdirectory(${llama_SOURCE_DIR} llama-build EXCLUDE_FROM_ALL)

add_library(pisar_llm SHARED pisar_llm.cpp)
target_include_directories(pisar_llm PRIVATE
    ${llama_SOURCE_DIR}/include
    ${llama_SOURCE_DIR}/ggml/include
    ${llama_SOURCE_DIR}/common)
target_link_libraries(pisar_llm PRIVATE llama llama-common log)
