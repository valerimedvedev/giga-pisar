<?php
/**
 * Модели на сервере сайта.
 *
 * Браузер не может скачать модель прямо с GitHub (GitHub не отдаёт файлы
 * релизов чужим сайтам), поэтому администратор один раз кладёт копию на
 * свой сервер — в uploads/giga-pisar/. Оттуда её и качают посетители.
 * Qwen можно тоже держать у себя, чтобы не зависеть от Hugging Face.
 */

defined( 'ABSPATH' ) || exit;

function giga_pisar_models_dir() {
	$u = wp_upload_dir( null, false );
	return trailingslashit( $u['basedir'] ) . 'giga-pisar/';
}

function giga_pisar_models_url() {
	$u = wp_upload_dir( null, false );
	return trailingslashit( $u['baseurl'] ) . 'giga-pisar/';
}

/** Что можно скачать: ключ → [файл, первоисточник, подпись]. */
function giga_pisar_model_catalog() {
	return array(
		'gigaam' => array( GIGA_PISAR_GIGAAM_FILE, GIGA_PISAR_GIGAAM_URL, __( 'Пакет распознавания речи GigaAM v3 (213 МБ)', 'giga-pisar' ) ),
		'qwen'   => array( GIGA_PISAR_QWEN_FILE, GIGA_PISAR_QWEN_URL, __( 'Мозг Qwen3-4B для браузера (1,9 ГБ)', 'giga-pisar' ) ),
	);
}

/** Размер файла модели на сервере или 0. */
function giga_pisar_model_size( $key ) {
	$c = giga_pisar_model_catalog();
	$f = giga_pisar_models_dir() . $c[ $key ][0];
	return file_exists( $f ) ? (int) filesize( $f ) : 0;
}

/** Адрес архива GigaAM для браузера: своя копия → адрес из настроек → нет. */
function giga_pisar_gigaam_url() {
	if ( giga_pisar_model_size( 'gigaam' ) > 0 ) {
		return giga_pisar_models_url() . GIGA_PISAR_GIGAAM_FILE;
	}
	return (string) giga_pisar_opt( 'model_url' );
}

/** Откуда браузер качает Qwen, по порядку. */
function giga_pisar_qwen_urls() {
	$urls = array();
	if ( giga_pisar_model_size( 'qwen' ) > 0 ) {
		$urls[] = giga_pisar_models_url() . GIGA_PISAR_QWEN_FILE;
	}
	if ( giga_pisar_opt( 'qwen_url' ) ) {
		$urls[] = giga_pisar_opt( 'qwen_url' );
	}
	$urls[] = GIGA_PISAR_QWEN_URL;
	return $urls;
}

// ─────────────────────────── действия администратора ───────────────────────────

add_action( 'admin_post_giga_pisar_fetch_model', 'giga_pisar_fetch_model' );
add_action( 'admin_post_giga_pisar_delete_model', 'giga_pisar_delete_model' );

function giga_pisar_model_action_guard() {
	if ( ! current_user_can( 'manage_options' ) ) {
		wp_die( esc_html__( 'Недостаточно прав.', 'giga-pisar' ), 403 );
	}
	check_admin_referer( 'giga_pisar_models' );
	$key = isset( $_POST['model'] ) ? sanitize_key( wp_unslash( $_POST['model'] ) ) : '';
	$c   = giga_pisar_model_catalog();
	if ( ! isset( $c[ $key ] ) ) {
		wp_die( esc_html__( 'Неизвестная модель.', 'giga-pisar' ), 400 );
	}
	return $key;
}

function giga_pisar_back( $notice ) {
	wp_safe_redirect( add_query_arg( array( 'page' => 'giga-pisar', 'tab' => 'models', 'gp_notice' => rawurlencode( $notice ) ), admin_url( 'admin.php' ) ) );
	exit;
}

/** Скачивает модель с первоисточника на сервер сайта. Долго — до десятков минут для Qwen. */
function giga_pisar_fetch_model() {
	$key = giga_pisar_model_action_guard();
	list( $file, $url ) = giga_pisar_model_catalog()[ $key ];

	// Человек может закрыть вкладку — докачаем всё равно.
	ignore_user_abort( true );
	if ( function_exists( 'set_time_limit' ) ) {
		set_time_limit( 0 );
	}
	require_once ABSPATH . 'wp-admin/includes/file.php';

	$dir = giga_pisar_models_dir();
	if ( ! wp_mkdir_p( $dir ) ) {
		giga_pisar_back( __( 'Не удалось создать папку в uploads.', 'giga-pisar' ) );
	}
	$tmp = download_url( $url, 3600 );
	if ( is_wp_error( $tmp ) ) {
		/* translators: %s: error message */
		giga_pisar_back( sprintf( __( 'Не скачалось: %s', 'giga-pisar' ), $tmp->get_error_message() ) );
	}
	if ( ! @rename( $tmp, $dir . $file ) && ! @copy( $tmp, $dir . $file ) ) { // phpcs:ignore WordPress.PHP.NoSilencedErrors
		wp_delete_file( $tmp );
		giga_pisar_back( __( 'Не удалось положить файл в uploads.', 'giga-pisar' ) );
	}
	if ( file_exists( $tmp ) ) {
		wp_delete_file( $tmp );
	}
	giga_pisar_back( __( 'Готово: модель лежит на сервере.', 'giga-pisar' ) );
}

function giga_pisar_delete_model() {
	$key  = giga_pisar_model_action_guard();
	$file = giga_pisar_models_dir() . giga_pisar_model_catalog()[ $key ][0];
	if ( file_exists( $file ) ) {
		wp_delete_file( $file );
	}
	giga_pisar_back( __( 'Модель удалена с сервера.', 'giga-pisar' ) );
}
