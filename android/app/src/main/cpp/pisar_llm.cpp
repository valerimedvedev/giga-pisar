// Нейронка на телефоне: тонкий мост JNI к llama.cpp.
//
// Один запрос — одна пара сообщений (системная инструкция + текст), ответ
// целиком: Писарю не нужна беседа, нужна быстрая правка. Шаблон чата
// берётся из самой модели (Qwen, Gemma, GigaChat — у каждой свой), думать
// вслух запрещаем (enable_thinking=false), <think>…</think> всё равно
// вырезается на стороне Kotlin.
//
// Проверено компилятором против llama.cpp v0.5.0 (см. CMakeLists.txt).
#include <jni.h>
#include <atomic>
#include <string>
#include <vector>

#include "chat.h"
#include "common.h"
#include "llama.h"

#ifdef __ANDROID__
#include <android/log.h>
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, "GigaPisar", __VA_ARGS__)
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, "GigaPisar", __VA_ARGS__)
#else
#include <cstdio>
#define LOGI(...) fprintf(stderr, __VA_ARGS__)
#define LOGE(...) fprintf(stderr, __VA_ARGS__)
#endif

namespace {

constexpr int BATCH = 512;

llama_model               *g_model   = nullptr;
llama_context             *g_ctx     = nullptr;
common_chat_templates_ptr  g_tmpls;
llama_batch                g_batch{};
int                        g_ctx_size = 4096;
std::atomic<bool>          g_cancel{false};

std::string jstr(JNIEnv *env, jstring s) {
    if (!s) return "";
    const char *c = env->GetStringUTFChars(s, nullptr);
    std::string out(c ? c : "");
    if (c) env->ReleaseStringUTFChars(s, c);
    return out;
}

void unload() {
    if (g_batch.token) { llama_batch_free(g_batch); g_batch = llama_batch{}; }
    g_tmpls.reset();
    if (g_ctx) { llama_free(g_ctx); g_ctx = nullptr; }
    if (g_model) { llama_model_free(g_model); g_model = nullptr; }
}

// Прогоняет токены через модель пачками; логиты — только для последнего.
bool decode(const std::vector<llama_token> &tokens, int start) {
    for (int i = 0; i < (int) tokens.size(); i += BATCH) {
        const int n = std::min((int) tokens.size() - i, BATCH);
        common_batch_clear(g_batch);
        for (int j = 0; j < n; j++) {
            common_batch_add(g_batch, tokens[i + j], start + i + j, {0}, i + j == (int) tokens.size() - 1);
        }
        if (llama_decode(g_ctx, g_batch) != 0) return false;
        if (g_cancel.load()) return false;
    }
    return true;
}

// Нормальный UTF-8 на границе токена — иначе Java подавится половиной символа.
bool utf8_complete(const std::string &s) {
    size_t i = 0;
    while (i < s.size()) {
        const unsigned char c = s[i];
        size_t n = c < 0x80 ? 1 : (c >> 5) == 6 ? 2 : (c >> 4) == 14 ? 3 : (c >> 3) == 30 ? 4 : 0;
        if (n == 0) return false;
        if (i + n > s.size()) return false;
        i += n;
    }
    return true;
}

} // namespace

extern "C" JNIEXPORT void JNICALL
Java_ru_gigapisar_app_LocalLlm_nativeInit(JNIEnv *, jclass) {
    llama_log_set([](ggml_log_level level, const char *text, void *) {
        if (level >= GGML_LOG_LEVEL_WARN) LOGI("%s", text);
    }, nullptr);
    llama_backend_init();
}

// 0 — успех, 1 — модель не открылась, 2 — контекст не создался.
extern "C" JNIEXPORT jint JNICALL
Java_ru_gigapisar_app_LocalLlm_nativeLoad(JNIEnv *env, jclass, jstring jpath, jint threads, jint ctx_size) {
    unload();
    const std::string path = jstr(env, jpath);
    llama_model_params mp = llama_model_default_params();
    g_model = llama_model_load_from_file(path.c_str(), mp);
    if (!g_model) { LOGE("модель не открылась: %s", path.c_str()); return 1; }

    g_ctx_size = ctx_size > 0 ? ctx_size : 4096;
    llama_context_params cp = llama_context_default_params();
    cp.n_ctx = g_ctx_size;
    cp.n_batch = BATCH;
    cp.n_ubatch = BATCH;
    cp.n_threads = threads > 0 ? threads : 4;
    cp.n_threads_batch = cp.n_threads;
    g_ctx = llama_init_from_model(g_model, cp);
    if (!g_ctx) { LOGE("контекст не создался"); unload(); return 2; }
    g_batch = llama_batch_init(BATCH, 0, 1);
    g_tmpls = common_chat_templates_init(g_model, "");
    LOGI("модель загружена: %s, потоков %d, контекст %d", path.c_str(), cp.n_threads, g_ctx_size);
    return 0;
}

extern "C" JNIEXPORT jboolean JNICALL
Java_ru_gigapisar_app_LocalLlm_nativeLoaded(JNIEnv *, jclass) {
    return g_ctx != nullptr;
}

extern "C" JNIEXPORT void JNICALL
Java_ru_gigapisar_app_LocalLlm_nativeCancel(JNIEnv *, jclass) {
    g_cancel.store(true);
}

extern "C" JNIEXPORT void JNICALL
Java_ru_gigapisar_app_LocalLlm_nativeUnload(JNIEnv *, jclass) {
    unload();
}

// Ответ целиком на беседу (роли и тексты — параллельные массивы).
// Пустая строка — ошибка или отмена (подробности в logcat).
static std::string complete(std::vector<common_chat_msg> msgs, int max_tokens, float temperature);

extern "C" JNIEXPORT jstring JNICALL
Java_ru_gigapisar_app_LocalLlm_nativeComplete(JNIEnv *env, jclass, jstring jsystem, jstring juser,
                                              jint max_tokens, jfloat temperature) {
    common_chat_msg sys, usr;
    sys.role = "system"; sys.content = jstr(env, jsystem);
    usr.role = "user";   usr.content = jstr(env, juser);
    return env->NewStringUTF(complete({sys, usr}, max_tokens, temperature).c_str());
}

extern "C" JNIEXPORT jstring JNICALL
Java_ru_gigapisar_app_LocalLlm_nativeCompleteChat(JNIEnv *env, jclass, jobjectArray roles, jobjectArray contents,
                                                  jint max_tokens, jfloat temperature) {
    std::vector<common_chat_msg> msgs;
    const int n = env->GetArrayLength(roles);
    for (int i = 0; i < n; i++) {
        common_chat_msg m;
        m.role = jstr(env, (jstring) env->GetObjectArrayElement(roles, i));
        m.content = jstr(env, (jstring) env->GetObjectArrayElement(contents, i));
        msgs.push_back(m);
    }
    return env->NewStringUTF(complete(msgs, max_tokens, temperature).c_str());
}

static std::string complete(std::vector<common_chat_msg> msgs, int max_tokens, float temperature) {
    if (!g_ctx || !g_model) return "";
    g_cancel.store(false);

    common_chat_templates_inputs in;
    in.messages = msgs;
    in.add_generation_prompt = true;
    in.use_jinja = true;
    in.enable_thinking = false;
    std::string prompt;
    try {
        prompt = common_chat_templates_apply(g_tmpls.get(), in).prompt;
    } catch (const std::exception &e) {
        LOGE("шаблон чата: %s — беру простой формат", e.what());
        for (const auto &m : msgs) prompt += m.role + ": " + m.content + "\n\n";
        prompt += "assistant: ";
    }

    std::vector<llama_token> tokens = common_tokenize(g_ctx, prompt, true, true);
    const int room = g_ctx_size - 8;
    if ((int) tokens.size() > room) {
        LOGE("текст слишком длинный для контекста: %d токенов", (int) tokens.size());
        return "";
    }
    llama_memory_clear(llama_get_memory(g_ctx), true);
    if (!decode(tokens, 0)) return "";
    int pos = (int) tokens.size();

    llama_sampler *smpl = llama_sampler_chain_init(llama_sampler_chain_default_params());
    llama_sampler_chain_add(smpl, llama_sampler_init_min_p(0.05f, 1));
    llama_sampler_chain_add(smpl, llama_sampler_init_temp(temperature));
    llama_sampler_chain_add(smpl, llama_sampler_init_dist(LLAMA_DEFAULT_SEED));

    const llama_vocab *vocab = llama_model_get_vocab(g_model);
    std::string out, pending;
    const int limit = std::min(max_tokens > 0 ? (int) max_tokens : 1024, room - pos);
    for (int i = 0; i < limit && !g_cancel.load(); i++) {
        const llama_token id = llama_sampler_sample(smpl, g_ctx, -1);
        llama_sampler_accept(smpl, id);
        if (llama_vocab_is_eog(vocab, id)) break;
        pending += common_token_to_piece(g_ctx, id, true);
        if (utf8_complete(pending)) { out += pending; pending.clear(); }
        common_batch_clear(g_batch);
        common_batch_add(g_batch, id, pos++, {0}, true);
        if (llama_decode(g_ctx, g_batch) != 0) { LOGE("llama_decode не удался"); break; }
    }
    llama_sampler_free(smpl);
    if (g_cancel.load()) return "";
    return out;
}
