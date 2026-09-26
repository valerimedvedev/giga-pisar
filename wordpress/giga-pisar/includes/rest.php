<?php
/**
 * REST: мозг GigaChat через сервер сайта.
 *
 * Браузер не ходит к llama-server напрямую: запрос идёт сюда, WordPress
 * проверяет права (мозг включил администратор и открыл этому человеку),
 * сам собирает промпт и только тогда спрашивает GigaChat. Поэтому снаружи
 * нельзя использовать нейронку как бесплатный чат — только для правки текста.
 */

defined( 'ABSPATH' ) || exit;

const GIGA_PISAR_MAX_TEXT    = 8000;  // знаков текста на одну правку
const GIGA_PISAR_MAX_COMMAND = 300;   // знаков в команде
const GIGA_PISAR_RATE        = 12;    // правок в минуту с одного человека

add_action( 'rest_api_init', 'giga_pisar_rest_routes' );

function giga_pisar_rest_routes() {
	register_rest_route(
		'giga-pisar/v1',
		'/brain',
		array(
			'methods'             => 'POST',
			'callback'            => 'giga_pisar_rest_brain',
			'permission_callback' => 'giga_pisar_rest_brain_allowed',
			'args'                => array(
				'body'    => array( 'type' => 'string', 'required' => true ),
				'command' => array( 'type' => 'string', 'required' => true ),
				'mode'    => array( 'type' => 'string', 'enum' => array( 'dictation', 'selection' ), 'default' => 'dictation' ),
			),
		)
	);
	register_rest_route(
		'giga-pisar/v1',
		'/brain/health',
		array(
			'methods'             => 'GET',
			'callback'            => 'giga_pisar_rest_health',
			'permission_callback' => 'giga_pisar_rest_brain_allowed',
		)
	);
}

function giga_pisar_rest_brain_allowed() {
	return giga_pisar_can_brain() && 'gigachat' === giga_pisar_opt( 'brain_provider' );
}

/** Заголовки к серверному мозгу: ключ API, если задан. */
function giga_pisar_upstream_headers() {
	$h   = array( 'Content-Type' => 'application/json' );
	$key = giga_pisar_opt( 'gigachat_key' );
	if ( $key ) {
		$h['Authorization'] = 'Bearer ' . $key;
	}
	return $h;
}

/** Проверка серверного мозга; ответ кешируется на 30 секунд. */
function giga_pisar_rest_health() {
	return array( 'status' => giga_pisar_gigachat_health() );
}

function giga_pisar_gigachat_health( $fresh = false ) {
	$cached = get_transient( 'giga_pisar_health' );
	if ( ! $fresh && false !== $cached ) {
		return $cached;
	}
	$svc = giga_pisar_opt( 'gigachat_service' );
	if ( 'llama' === $svc || ! $svc ) {
		$r = wp_remote_get( giga_pisar_opt( 'gigachat_url' ) . 'health', array( 'timeout' => 5 ) );
	} elseif ( 'cloudflare' === $svc ) {
		// списка моделей нет — считаем живым, если ключ задан
		$r = giga_pisar_opt( 'gigachat_key' ) ? array( 'response' => array( 'code' => 200 ) ) : new WP_Error( 'nokey', 'nokey' );
	} else {
		$r = wp_remote_get( giga_pisar_opt( 'gigachat_url' ) . 'v1/models', array( 'timeout' => 8, 'headers' => giga_pisar_upstream_headers() ) );
	}
	$code   = is_wp_error( $r ) ? 0 : (int) wp_remote_retrieve_response_code( $r );
	$status = 200 === $code ? 'ok' : ( 503 === $code ? 'loading' : 'absent' );
	set_transient( 'giga_pisar_health', $status, 30 );
	return $status;
}

// Промпты — giga_pisar_prompt() в options.php: из настроек или вшитые.

function giga_pisar_rest_brain( WP_REST_Request $req ) {
	$body    = trim( (string) $req['body'] );
	$command = trim( wp_strip_all_tags( (string) $req['command'] ) );
	$mode    = (string) $req['mode'];

	if ( '' === $body || '' === $command ) {
		return new WP_Error( 'giga_pisar_empty', __( 'Нет текста или команды.', 'giga-pisar' ), array( 'status' => 400 ) );
	}
	if ( mb_strlen( $body ) > GIGA_PISAR_MAX_TEXT || mb_strlen( $command ) > GIGA_PISAR_MAX_COMMAND ) {
		return new WP_Error( 'giga_pisar_long', __( 'Слишком длинный текст для одной правки.', 'giga-pisar' ), array( 'status' => 413 ) );
	}

	// лимит: по пользователю, а у гостей — по адресу
	$who  = get_current_user_id() ? 'u' . get_current_user_id() : 'ip' . md5( $_SERVER['REMOTE_ADDR'] ?? '' ); // phpcs:ignore
	$key  = 'giga_pisar_rate_' . $who;
	$used = (int) get_transient( $key );
	if ( $used >= GIGA_PISAR_RATE ) {
		return new WP_Error( 'giga_pisar_rate', __( 'Слишком много запросов подряд, подождите минуту.', 'giga-pisar' ), array( 'status' => 429 ) );
	}
	set_transient( $key, $used + 1, MINUTE_IN_SECONDS );

	$svc     = giga_pisar_opt( 'gigachat_service' );
	$payload = array(
		'messages'    => array(
			array( 'role' => 'system', 'content' => giga_pisar_prompt( $mode ) . "\n\nКоманда пользователя к тексту: {$command}." ),
			array( 'role' => 'user', 'content' => $body ),
		),
		'temperature' => 0.3,
		'max_tokens'  => 2048,
	);
	if ( giga_pisar_opt( 'gigachat_model' ) ) {
		$payload['model'] = giga_pisar_opt( 'gigachat_model' );
	}
	if ( 'llama' === $svc || ! $svc ) {
		$payload['chat_template_kwargs'] = array( 'enable_thinking' => false ); // поле llama.cpp; облака его отвергают
	} else {
		$payload = array_merge( $payload, giga_pisar_cloud_services()[ $svc ]['extras'] ?? array() ); // например, reasoning_effort у Gemini
	}
	$r = wp_remote_post(
		giga_pisar_opt( 'gigachat_url' ) . 'v1/chat/completions',
		array(
			'timeout' => 120,
			'headers' => giga_pisar_upstream_headers(),
			'body'    => wp_json_encode( $payload ),
		)
	);
	if ( is_wp_error( $r ) ) {
		return new WP_Error( 'giga_pisar_upstream', sprintf( __( '%s не ответил.', 'giga-pisar' ), giga_pisar_server_brain_label() ), array( 'status' => 502 ) );
	}
	$code = (int) wp_remote_retrieve_response_code( $r );
	$json = json_decode( wp_remote_retrieve_body( $r ), true );
	if ( isset( $json[0] ) && is_array( $json[0] ) ) {
		$json = $json[0]; // Gemini заворачивает ошибку в массив
	}
	$text = $json['choices'][0]['message']['content'] ?? '';
	// если модель подумала вслух — оставляем только ответ
	$pos = strpos( $text, '</think>' );
	if ( false !== $pos ) {
		$text = substr( $text, $pos + 8 );
	}
	$text = trim( $text );
	if ( 200 !== $code || '' === $text ) {
		$why = isset( $json['error']['message'] ) ? ': ' . mb_substr( (string) $json['error']['message'], 0, 200 ) : '';
		/* translators: 1: service name, 2: HTTP status, 3: message */
		return new WP_Error( 'giga_pisar_upstream', sprintf( __( '%1$s ответил ошибкой (%2$d)%3$s', 'giga-pisar' ), giga_pisar_server_brain_label(), $code, $why ), array( 'status' => 502 ) );
	}
	return array( 'text' => $text );
}
