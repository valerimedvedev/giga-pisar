<?php
/**
 * Plugin Name:       Гига Писарь — голосовой ввод
 * Plugin URI:        https://github.com/valerimedvedev/giga-pisar
 * Description:       Диктовка голосом в любое поле ввода и в редакторы WordPress. Речь распознаётся моделью GigaAM прямо в браузере — звук никуда не уходит. Мозг Писаря (очистка речи нейронкой Qwen или GigaChat) — по решению администратора.
 * Version:           1.3.0
 * Requires at least: 6.2
 * Requires PHP:      7.4
 * Author:            Гига Писарь
 * License:           MIT
 * Text Domain:       giga-pisar
 */

defined( 'ABSPATH' ) || exit;

define( 'GIGA_PISAR_VERSION', '1.3.0' );
define( 'GIGA_PISAR_FILE', __FILE__ );
define( 'GIGA_PISAR_DIR', plugin_dir_path( __FILE__ ) );
define( 'GIGA_PISAR_URL', plugin_dir_url( __FILE__ ) );

/** Файлы моделей, которые администратор может положить на свой сервер. */
define( 'GIGA_PISAR_GIGAAM_FILE', 'gigaam-v3-onnx-int8.tar.gz' );
define( 'GIGA_PISAR_GIGAAM_URL', 'https://github.com/moznoazachem/giga-pisar-cli/releases/download/v1.0/gigaam-v3-onnx-int8.tar.gz' );
define( 'GIGA_PISAR_QWEN_FILE', 'Qwen3-4B-Instruct-2507-Q3_K_M.gguf' );
define( 'GIGA_PISAR_QWEN_URL', 'https://huggingface.co/unsloth/Qwen3-4B-Instruct-2507-GGUF/resolve/main/Qwen3-4B-Instruct-2507-Q3_K_M.gguf' );

require_once GIGA_PISAR_DIR . 'includes/options.php';
require_once GIGA_PISAR_DIR . 'includes/models.php';
require_once GIGA_PISAR_DIR . 'includes/rest.php';
require_once GIGA_PISAR_DIR . 'includes/assets.php';
require_once GIGA_PISAR_DIR . 'includes/admin.php';

register_uninstall_hook( __FILE__, 'giga_pisar_uninstall' );

/** Удаление плагина: настройки и выложенные на сервер модели. */
function giga_pisar_uninstall() {
	delete_option( 'giga_pisar_options' );
	$dir = giga_pisar_models_dir();
	foreach ( array( GIGA_PISAR_GIGAAM_FILE, GIGA_PISAR_QWEN_FILE ) as $file ) {
		if ( file_exists( $dir . $file ) ) {
			wp_delete_file( $dir . $file );
		}
	}
}
