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
	);
}

function giga_pisar_options() {
	$saved = get_option( 'giga_pisar_options', array() );
	return array_merge( giga_pisar_defaults(), is_array( $saved ) ? $saved : array() );
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
		'brain'   => array( 'brain' ),
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
