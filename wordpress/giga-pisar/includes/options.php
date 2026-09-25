<?php
/**
 * Настройки плагина и права: кто может диктовать, кому доступен мозг.
 *
 * Диктовка (распознавание в браузере) — всем, кому её открыл администратор.
 * Мозг (очистка речи и команды «Писарь, …») включает и выключает только
 * администратор, и он же решает, кому мозг доступен.
 */

defined( 'ABSPATH' ) || exit;

function giga_pisar_defaults() {
	return array(
		'frontend'       => 1,          // диктовка на сайте
		'admin'          => 1,          // диктовка в админке
		'floating'       => 1,          // плавающая кнопка у полей ввода
		'editor_buttons' => 1,          // кнопка «Диктовка» в редакторах
		'who'            => 'all',      // all | logged_in — кто может диктовать
		'brain'          => 0,          // мозг: очистка речи и команды
		'brain_provider' => 'qwen',     // qwen (в браузере) | gigachat (на сервере)
		'brain_who'      => 'admins',   // admins | dictation — кому доступен мозг
		'gigachat_url'   => '',         // llama-server с GigaChat (OpenAI-совместимый)
		'model_url'      => '',         // свой адрес архива GigaAM
		'qwen_url'       => '',         // свой адрес Qwen .gguf
		'isolation'      => 0,          // заголовки COOP/COEP — многопоточность
		'brain_local'    => 1,          // разрешить мозг на компьютере пользователя
		'chips'          => giga_pisar_default_chips(),   // функции мозга на кнопках
		'prompt_dictation' => '',       // пусто = вшитый промпт
		'prompt_selection' => '',
	);
}

const GIGA_PISAR_MAX_CHIPS = 10;

/** Функции мозга по умолчанию (кнопки над текстом). Те же, что в assets/giga/brain.js. */
function giga_pisar_default_chips() {
	return array(
		array( 'title' => 'Причесать', 'command' => 'причеши текст: убери слова-паразиты и повторы, поправь пунктуацию и очевидные ошибки; смысл, порядок мыслей и лексику не меняй' ),
		array( 'title' => 'Исправить ошибки', 'command' => 'исправь только орфографию, пунктуацию и ошибки распознавания; слова, порядок и стиль не меняй' ),
		array( 'title' => 'Сократить', 'command' => 'сократи, сохранив суть' ),
		array( 'title' => 'Собрать мысль', 'command' => 'собери мысль: из сбивчивой речи сделай связный текст в том же порядке мыслей, ничего не добавляя от себя' ),
		array( 'title' => 'Сгладить', 'command' => 'сгладь тон: сделай мягче и вежливее, убери резкость; смысл не меняй' ),
		array( 'title' => 'Деловой стиль', 'command' => 'перепиши в деловом стиле: нейтрально, чётко, без разговорных слов; смысл не меняй' ),
		array( 'title' => 'Перевести на английский', 'command' => 'переведи на английский' ),
	);
}

/** Вшитые промпты — образец и запасной вариант, если поле в настройках пустое. */
function giga_pisar_default_prompts() {
	return array(
		'dictation' => 'Ты обрабатываешь надиктованный голосом текст перед вставкой. Правила: '
			. 'убери слова-паразиты и оговорки (э, ну, типа, вот, как бы), убери повторы '
			. 'и самоисправления, расставь знаки препинания, исправь очевидные ошибки '
			. 'распознавания. Сохраняй смысл и лексику, ничего не добавляй от себя и '
			. 'не комментируй. Живой тон автора сохраняй, если только команда не велит '
			. 'его изменить: команда важнее тона. Выполни команду пользователя: она '
			. 'дана в конце этой инструкции, в сам текст не входит, и упоминать её '
			. 'в ответе нельзя. Верни ТОЛЬКО готовый текст, без кавычек вокруг него.',
		'selection' => 'Ты редактируешь текст, который пользователь выделил в своём документе, '
			. 'и выполняешь над ним команду пользователя. Сохраняй смысл и разбиение '
			. 'на абзацы, ничего не добавляй от себя и не комментируй. Тон и стиль '
			. 'сохраняй, если только команда не велит их изменить: команда важнее. '
			. 'Команда дана в конце этой инструкции, в сам текст не входит, и '
			. 'упоминать её в ответе нельзя. Верни ТОЛЬКО готовый текст, без кавычек '
			. 'вокруг него.',
	);
}

/** Промпт для режима: из настроек, а если там пусто — вшитый. */
function giga_pisar_prompt( $mode ) {
	$key = 'selection' === $mode ? 'prompt_selection' : 'prompt_dictation';
	$own = trim( (string) giga_pisar_opt( $key ) );
	return '' !== $own ? $own : giga_pisar_default_prompts()[ 'selection' === $mode ? 'selection' : 'dictation' ];
}

/** Строки команд из формы → чистый список [{title, command}], не больше GIGA_PISAR_MAX_CHIPS. */
function giga_pisar_sanitize_chips( $rows ) {
	$out  = array();
	$seen = array();
	foreach ( (array) $rows as $row ) {
		if ( ! is_array( $row ) ) {
			continue;
		}
		$title   = sanitize_text_field( $row['title'] ?? '' );
		$command = sanitize_textarea_field( $row['command'] ?? '' );
		if ( '' === $title || '' === $command || isset( $seen[ mb_strtolower( $title ) ] ) ) {
			continue;
		}
		$seen[ mb_strtolower( $title ) ] = true;
		$out[] = array( 'title' => mb_substr( $title, 0, 40 ), 'command' => mb_substr( $command, 0, 300 ) );
		if ( count( $out ) >= GIGA_PISAR_MAX_CHIPS ) {
			break;
		}
	}
	return $out;
}

function giga_pisar_options() {
	$saved = get_option( 'giga_pisar_options', array() );
	$o     = array_merge( giga_pisar_defaults(), is_array( $saved ) ? $saved : array() );
	// До 1.2 chips хранил список id вшитых функций — переводим в строки.
	if ( ! empty( $o['chips'] ) && ! is_array( reset( $o['chips'] ) ) ) {
		$ids = array( 'tidy', 'fix', 'short', 'compose', 'smooth', 'formal', 'english' );
		$def = giga_pisar_default_chips();
		$o['chips'] = array_values( array_filter( array_map( function ( $id ) use ( $ids, $def ) {
			$i = array_search( $id, $ids, true );
			return false === $i ? null : $def[ $i ];
		}, (array) $o['chips'] ) ) );
	}
	return $o;
}

function giga_pisar_opt( $key ) {
	$o = giga_pisar_options();
	return $o[ $key ] ?? null;
}

/** Проверка и чистка того, что пришло из формы настроек. */
function giga_pisar_sanitize_options( $in ) {
	$d   = giga_pisar_defaults();
	$out = giga_pisar_options();
	$tab = isset( $in['_tab'] ) ? sanitize_key( $in['_tab'] ) : '';

	// флажки: отсутствие в форме = выключено, но только для полей своей вкладки
	$checkboxes = array(
		'general' => array( 'frontend', 'admin', 'floating', 'editor_buttons', 'isolation' ),
		'brain'   => array( 'brain', 'brain_local' ),
	);
	foreach ( $checkboxes[ $tab ] ?? array() as $k ) {
		$out[ $k ] = empty( $in[ $k ] ) ? 0 : 1;
	}
	// Без вкладки — это уже готовый массив настроек (при первом сохранении
	// WordPress прогоняет его через эту функцию второй раз): флажки берём как есть.
	if ( '' === $tab ) {
		foreach ( array_merge( ...array_values( $checkboxes ) ) as $k ) {
			if ( isset( $in[ $k ] ) ) {
				$out[ $k ] = empty( $in[ $k ] ) ? 0 : 1;
			}
		}
	}
	if ( isset( $in['chips'] ) && is_array( $in['chips'] ) ) {
		$out['chips'] = giga_pisar_sanitize_chips( $in['chips'] );
	} elseif ( 'brain' === $tab && ! empty( $in['chips_reset'] ) ) {
		$out['chips'] = giga_pisar_default_chips();
	}
	foreach ( array( 'prompt_dictation', 'prompt_selection' ) as $k ) {
		if ( isset( $in[ $k ] ) ) {
			$v = trim( sanitize_textarea_field( $in[ $k ] ) );
			// сохранили образец без изменений — считаем, что поле пустое (вшитый промпт)
			$def       = giga_pisar_default_prompts()[ 'prompt_selection' === $k ? 'selection' : 'dictation' ];
			$out[ $k ] = $v === $def ? '' : mb_substr( $v, 0, 3000 );
		}
	}
	if ( isset( $in['who'] ) ) {
		$out['who'] = in_array( $in['who'], array( 'all', 'logged_in' ), true ) ? $in['who'] : $d['who'];
	}
	if ( isset( $in['brain_provider'] ) ) {
		$out['brain_provider'] = in_array( $in['brain_provider'], array( 'qwen', 'gigachat' ), true ) ? $in['brain_provider'] : $d['brain_provider'];
	}
	if ( isset( $in['brain_who'] ) ) {
		$out['brain_who'] = in_array( $in['brain_who'], array( 'admins', 'dictation' ), true ) ? $in['brain_who'] : $d['brain_who'];
	}
	foreach ( array( 'gigachat_url', 'model_url', 'qwen_url' ) as $k ) {
		if ( isset( $in[ $k ] ) ) {
			$url       = esc_url_raw( trim( $in[ $k ] ), array( 'http', 'https' ) );
			$out[ $k ] = ( 'gigachat_url' === $k && $url ) ? trailingslashit( $url ) : $url;
		}
	}
	return $out;
}

/** Может ли текущий посетитель диктовать здесь (на сайте или в админке). */
function giga_pisar_can_dictate( $context = null ) {
	$o       = giga_pisar_options();
	$context = $context ?? ( is_admin() ? 'admin' : 'frontend' );
	if ( empty( $o[ $context ] ) ) {
		return false;
	}
	return 'all' === $o['who'] || is_user_logged_in();
}

/** Доступен ли текущему пользователю мозг Писаря. */
function giga_pisar_can_brain() {
	$o = giga_pisar_options();
	if ( empty( $o['brain'] ) ) {
		return false;
	}
	if ( 'gigachat' === $o['brain_provider'] && empty( $o['gigachat_url'] ) ) {
		return false;
	}
	if ( 'admins' === $o['brain_who'] ) {
		return current_user_can( 'manage_options' );
	}
	return giga_pisar_can_dictate( 'frontend' ) || giga_pisar_can_dictate( 'admin' );
}
