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

/** Проверка llama-server; ответ кешируется на 30 секунд. */
function giga_pisar_rest_health() {
	return array( 'status' => giga_pisar_gigachat_health() );
}

function giga_pisar_gigachat_health( $fresh = false ) {
	$cached = get_transient( 'giga_pisar_health' );
	if ( ! $fresh && false !== $cached ) {
		return $cached;
	}
	$r = wp_remote_get( giga_pisar_opt( 'gigachat_url' ) . 'health', array( 'timeout' => 5 ) );
	$code   = is_wp_error( $r ) ? 0 : (int) wp_remote_retrieve_response_code( $r );
	$status = 200 === $code ? 'ok' : ( 503 === $code ? 'loading' : 'absent' );
	set_transient( 'giga_pisar_health', $status, 30 );
	return $status;
}

function giga_pisar_prompt( $mode ) {
	if ( 'selection' === $mode ) {
		return 'Ты редактируешь текст, который пользователь выделил в своём документе, '
			. 'и выполняешь над ним команду пользователя. Сохраняй смысл и разбиение '
			. 'на абзацы, ничего не добавляй от себя и не комментируй. Тон и стиль '
			. 'сохраняй, если только команда не велит их изменить: команда важнее. '
			. 'Команда дана в конце этой инструкции, в сам текст не входит, и '
			. 'упоминать её в ответе нельзя. Верни ТОЛЬКО готовый текст, без кавычек '
			. 'вокруг него.';
	}
	return 'Ты обрабатываешь надиктованный голосом текст перед вставкой. Правила: '
		. 'убери слова-паразиты и оговорки (э, ну, типа, вот, как бы), убери повторы '
		. 'и самоисправления, расставь знаки препинания, исправь очевидные ошибки '
		. 'распознавания. Сохраняй смысл и лексику, ничего не добавляй от себя и '
		. 'не комментируй. Живой тон автора сохраняй, если только команда не велит '
		. 'его изменить: команда важнее тона. Выполни команду пользователя: она '
		. 'дана в конце этой инструкции, в сам текст не входит, и упоминать её '
		. 'в ответе нельзя. Верни ТОЛЬКО готовый текст, без кавычек вокруг него.';
}

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

	$payload = array(
		'messages'             => array(
			array( 'role' => 'system', 'content' => giga_pisar_prompt( $mode ) . "\n\nКоманда пользователя к тексту: {$command}." ),
			array( 'role' => 'user', 'content' => $body ),
		),
		'temperature'          => 0.3,
		'max_tokens'           => 2048,
		'chat_template_kwargs' => array( 'enable_thinking' => false ),
	);
	$r = wp_remote_post(
		giga_pisar_opt( 'gigachat_url' ) . 'v1/chat/completions',
		array(
			'timeout' => 120,
			'headers' => array( 'Content-Type' => 'application/json' ),
			'body'    => wp_json_encode( $payload ),
		)
	);
	if ( is_wp_error( $r ) ) {
		return new WP_Error( 'giga_pisar_upstream', __( 'GigaChat не ответил.', 'giga-pisar' ), array( 'status' => 502 ) );
	}
	$code = (int) wp_remote_retrieve_response_code( $r );
	$json = json_decode( wp_remote_retrieve_body( $r ), true );
	$text = $json['choices'][0]['message']['content'] ?? '';
	// если модель подумала вслух — оставляем только ответ
	$pos = strpos( $text, '</think>' );
	if ( false !== $pos ) {
		$text = substr( $text, $pos + 8 );
	}
	$text = trim( $text );
	if ( 200 !== $code || '' === $text ) {
		/* translators: %d: HTTP status */
		return new WP_Error( 'giga_pisar_upstream', sprintf( __( 'GigaChat ответил ошибкой (%d).', 'giga-pisar' ), $code ), array( 'status' => 502 ) );
	}
	return array( 'text' => $text );
}
