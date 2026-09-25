<?php
/**
 * Подключение скриптов: диктовка (ES-модуль), кнопки в редакторах,
 * настройки для браузера и заголовки изоляции.
 */

defined( 'ABSPATH' ) || exit;

add_action( 'wp_enqueue_scripts', 'giga_pisar_enqueue_frontend' );
add_action( 'admin_enqueue_scripts', 'giga_pisar_enqueue_admin' );
add_action( 'enqueue_block_editor_assets', 'giga_pisar_enqueue_block_editor' );
add_filter( 'script_loader_tag', 'giga_pisar_module_tag', 10, 2 );
add_filter( 'mce_external_plugins', 'giga_pisar_mce_plugin' );
add_filter( 'mce_buttons', 'giga_pisar_mce_button' );
add_action( 'send_headers', 'giga_pisar_isolation_headers' );
add_action( 'admin_init', 'giga_pisar_isolation_headers' );

function giga_pisar_enqueue_frontend() {
	if ( giga_pisar_can_dictate( 'frontend' ) ) {
		giga_pisar_enqueue_core();
	}
}

function giga_pisar_enqueue_admin() {
	if ( giga_pisar_can_dictate( 'admin' ) ) {
		giga_pisar_enqueue_core();
	}
}

/** Настройки для браузера: что можно этому человеку и откуда брать модели. */
function giga_pisar_client_config() {
	$o = giga_pisar_options();
	return array(
		'version'    => GIGA_PISAR_VERSION,
		'floating'   => (bool) $o['floating'],
		'modelUrl'   => giga_pisar_gigaam_url(),
		'archiveUrl' => GIGA_PISAR_GIGAAM_URL,
		'brain'      => giga_pisar_can_brain() ? $o['brain_provider'] : null,
		'chips'      => array_values( (array) $o['chips'] ),
		'qwenUrls'   => giga_pisar_qwen_urls(),
		'restUrl'    => esc_url_raw( rest_url( 'giga-pisar/v1/' ) ),
		'nonce'      => wp_create_nonce( 'wp_rest' ),
		'isAdmin'    => current_user_can( 'manage_options' ),
		'settingsUrl' => admin_url( 'admin.php?page=giga-pisar' ),
	);
}

function giga_pisar_enqueue_core() {
	static $done = false;
	if ( $done ) {
		return;
	}
	$done = true;

	// Настройки — обычным скриптом до модуля (модули выполняются позже).
	wp_register_script( 'giga-pisar-config', false, array(), GIGA_PISAR_VERSION, false );
	wp_enqueue_script( 'giga-pisar-config' );
	wp_add_inline_script( 'giga-pisar-config', 'window.GigaPisarConfig = ' . wp_json_encode( giga_pisar_client_config() ) . ';' );

	wp_enqueue_script( 'giga-pisar', GIGA_PISAR_URL . 'assets/pisar-wp.js', array( 'giga-pisar-config' ), GIGA_PISAR_VERSION, true );
}

/** Наш главный скрипт — ES-модуль: ему нужны import и воркеры-модули. */
function giga_pisar_module_tag( $tag, $handle ) {
	if ( 'giga-pisar' === $handle && false === strpos( $tag, 'type="module"' ) ) {
		$tag = preg_replace( '/\stype=([\'"])text\/javascript\1/', '', $tag, 1 );
		// Плагины оптимизации (LiteSpeed, WP Rocket, Autoptimize, Cloudflare) любят
		// склеивать и откладывать скрипты — модуль после этого не работает.
		$tag = str_replace( '<script ', '<script type="module" data-no-optimize="1" data-no-defer="1" data-no-minify="1" data-cfasync="false" ', $tag );
	}
	return $tag;
}

// Те же исключения для плагинов оптимизации — через их собственные фильтры.
function giga_pisar_optimizer_excludes( $list ) {
	$list   = is_array( $list ) ? $list : array_filter( array_map( 'trim', explode( ',', (string) $list ) ) );
	$list[] = 'giga-pisar';
	return $list;
}
add_filter( 'litespeed_optimize_js_excludes', 'giga_pisar_optimizer_excludes' );
add_filter( 'litespeed_optm_js_defer_exc', 'giga_pisar_optimizer_excludes' );
add_filter( 'rocket_exclude_js', 'giga_pisar_optimizer_excludes' );
add_filter( 'rocket_exclude_defer_js', 'giga_pisar_optimizer_excludes' );
add_filter( 'rocket_delay_js_exclusions', 'giga_pisar_optimizer_excludes' );
add_filter( 'autoptimize_filter_js_exclude', function ( $exclude ) {
	return trim( $exclude . ', giga-pisar', ', ' );
} );

function giga_pisar_editor_buttons_enabled() {
	return giga_pisar_opt( 'editor_buttons' ) && giga_pisar_can_dictate();
}

/** Кнопка «Диктовка» в панели форматирования блочного редактора. */
function giga_pisar_enqueue_block_editor() {
	if ( ! giga_pisar_editor_buttons_enabled() ) {
		return;
	}
	giga_pisar_enqueue_core();
	wp_enqueue_script(
		'giga-pisar-block',
		GIGA_PISAR_URL . 'assets/editor-block.js',
		array( 'wp-rich-text', 'wp-block-editor', 'wp-blocks', 'wp-components', 'wp-compose', 'wp-data', 'wp-element', 'wp-hooks' ),
		GIGA_PISAR_VERSION,
		true
	);
}

/** Кнопка «Диктовка» в классическом редакторе (TinyMCE). */
function giga_pisar_mce_plugin( $plugins ) {
	if ( giga_pisar_editor_buttons_enabled() ) {
		giga_pisar_enqueue_core();
		$plugins['giga_pisar'] = GIGA_PISAR_URL . 'assets/editor-tinymce.js?ver=' . GIGA_PISAR_VERSION;
	}
	return $plugins;
}

function giga_pisar_mce_button( $buttons ) {
	if ( giga_pisar_editor_buttons_enabled() ) {
		$buttons[] = 'giga_pisar';
	}
	return $buttons;
}

/**
 * Изоляция страницы: с ней браузер даёт WebAssembly-потоки, и распознавание
 * идёт в несколько ядер. credentialless вместо require-corp — чтобы не
 * ломались картинки и встраивания с чужих сайтов. Включает администратор.
 */
function giga_pisar_isolation_headers() {
	if ( ! giga_pisar_opt( 'isolation' ) || headers_sent() ) {
		return;
	}
	if ( is_admin() ? ! giga_pisar_can_dictate( 'admin' ) : ! giga_pisar_can_dictate( 'frontend' ) ) {
		return;
	}
	header( 'Cross-Origin-Opener-Policy: same-origin' );
	header( 'Cross-Origin-Embedder-Policy: credentialless' );
}
