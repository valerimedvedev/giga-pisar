<?php
/**
 * Страница настроек — отдельный пункт «Гига Писарь» в меню администратора.
 * Вкладки: Диктовка, Мозг, Модели на сервере.
 */

defined( 'ABSPATH' ) || exit;

add_action( 'admin_menu', 'giga_pisar_admin_menu' );
add_action( 'admin_init', 'giga_pisar_register_setting' );
add_filter( 'plugin_action_links_' . plugin_basename( GIGA_PISAR_FILE ), 'giga_pisar_action_links' );

function giga_pisar_admin_menu() {
	add_menu_page(
		__( 'Гига Писарь', 'giga-pisar' ),
		__( 'Гига Писарь', 'giga-pisar' ),
		'manage_options',
		'giga-pisar',
		'giga_pisar_settings_page',
		'dashicons-microphone',
		81
	);
}

function giga_pisar_register_setting() {
	register_setting(
		'giga_pisar',
		'giga_pisar_options',
		array( 'sanitize_callback' => 'giga_pisar_sanitize_options' )
	);
}

function giga_pisar_action_links( $links ) {
	array_unshift( $links, '<a href="' . esc_url( admin_url( 'admin.php?page=giga-pisar' ) ) . '">' . esc_html__( 'Настройки', 'giga-pisar' ) . '</a>' );
	return $links;
}

function giga_pisar_size_text( $bytes ) {
	return $bytes ? size_format( $bytes, 1 ) : __( 'нет', 'giga-pisar' );
}

function giga_pisar_settings_page() {
	if ( ! current_user_can( 'manage_options' ) ) {
		return;
	}
	$o    = giga_pisar_options();
	$tabs = array(
		'general' => __( 'Диктовка', 'giga-pisar' ),
		'brain'   => __( 'Мозг', 'giga-pisar' ),
		'models'  => __( 'Модели на сервере', 'giga-pisar' ),
	);
	$tab = isset( $_GET['tab'] ) ? sanitize_key( wp_unslash( $_GET['tab'] ) ) : 'general'; // phpcs:ignore WordPress.Security.NonceVerification
	if ( ! isset( $tabs[ $tab ] ) ) {
		$tab = 'general';
	}
	$name = function ( $k ) {
		return 'giga_pisar_options[' . $k . ']';
	};
	?>
	<div class="wrap">
		<h1><span class="dashicons dashicons-microphone" style="font-size:28px;width:28px;height:28px;margin-right:6px"></span><?php esc_html_e( 'Гига Писарь — голосовой ввод', 'giga-pisar' ); ?></h1>

		<?php if ( ! empty( $_GET['gp_notice'] ) ) : // phpcs:ignore WordPress.Security.NonceVerification ?>
			<div class="notice notice-info is-dismissible"><p><?php echo esc_html( sanitize_text_field( wp_unslash( $_GET['gp_notice'] ) ) ); // phpcs:ignore WordPress.Security.NonceVerification ?></p></div>
		<?php endif; ?>
		<?php if ( ! giga_pisar_gigaam_url() ) : ?>
			<div class="notice notice-warning"><p>
				<?php esc_html_e( 'Пакет распознавания ещё не выложен на сервер. Пока его нет, посетители будут выбирать архив со своего диска. Выложите его на вкладке «Модели на сервере».', 'giga-pisar' ); ?>
			</p></div>
		<?php endif; ?>

		<nav class="nav-tab-wrapper">
			<?php foreach ( $tabs as $k => $label ) : ?>
				<a href="<?php echo esc_url( add_query_arg( array( 'page' => 'giga-pisar', 'tab' => $k ), admin_url( 'admin.php' ) ) ); ?>"
					class="nav-tab <?php echo $tab === $k ? 'nav-tab-active' : ''; ?>"><?php echo esc_html( $label ); ?></a>
			<?php endforeach; ?>
		</nav>

		<?php if ( 'general' === $tab ) : ?>
			<?php giga_pisar_diagnostics_box(); ?>
		<?php endif; ?>

		<?php if ( 'models' === $tab ) : ?>
			<?php giga_pisar_models_tab(); ?>
		<?php else : ?>
		<form method="post" action="options.php">
			<?php settings_fields( 'giga_pisar' ); ?>
			<input type="hidden" name="<?php echo esc_attr( $name( '_tab' ) ); ?>" value="<?php echo esc_attr( $tab ); ?>">
			<table class="form-table" role="presentation">
			<?php if ( 'general' === $tab ) : ?>
				<tr><th scope="row"><?php esc_html_e( 'Где работает диктовка', 'giga-pisar' ); ?></th><td>
					<label><input type="checkbox" name="<?php echo esc_attr( $name( 'frontend' ) ); ?>" value="1" <?php checked( $o['frontend'] ); ?>> <?php esc_html_e( 'На сайте', 'giga-pisar' ); ?></label><br>
					<label><input type="checkbox" name="<?php echo esc_attr( $name( 'admin' ) ); ?>" value="1" <?php checked( $o['admin'] ); ?>> <?php esc_html_e( 'В админке', 'giga-pisar' ); ?></label>
				</td></tr>
				<tr><th scope="row"><?php esc_html_e( 'Кнопки', 'giga-pisar' ); ?></th><td>
					<label><input type="checkbox" name="<?php echo esc_attr( $name( 'floating' ) ); ?>" value="1" <?php checked( $o['floating'] ); ?>> <?php esc_html_e( 'Плавающая кнопка микрофона у поля ввода, где стоит курсор', 'giga-pisar' ); ?></label><br>
					<label><input type="checkbox" name="<?php echo esc_attr( $name( 'editor_buttons' ) ); ?>" value="1" <?php checked( $o['editor_buttons'] ); ?>> <?php esc_html_e( 'Кнопка «Диктовка» в панели форматирования редакторов (блочного и классического)', 'giga-pisar' ); ?></label>
				</td></tr>
				<tr><th scope="row"><?php esc_html_e( 'Кто может диктовать', 'giga-pisar' ); ?></th><td>
					<label><input type="radio" name="<?php echo esc_attr( $name( 'who' ) ); ?>" value="all" <?php checked( $o['who'], 'all' ); ?>> <?php esc_html_e( 'Все посетители', 'giga-pisar' ); ?></label><br>
					<label><input type="radio" name="<?php echo esc_attr( $name( 'who' ) ); ?>" value="logged_in" <?php checked( $o['who'], 'logged_in' ); ?>> <?php esc_html_e( 'Только вошедшие пользователи', 'giga-pisar' ); ?></label>
					<p class="description"><?php esc_html_e( 'При первой диктовке человек подтверждает, что согласен скачать пакет распознавания (213 МБ) к себе в браузер. Речь распознаётся на его компьютере — звук никуда не отправляется.', 'giga-pisar' ); ?></p>
				</td></tr>
				<tr><th scope="row"><?php esc_html_e( 'Многопоточность', 'giga-pisar' ); ?></th><td>
					<label><input type="checkbox" name="<?php echo esc_attr( $name( 'isolation' ) ); ?>" value="1" <?php checked( $o['isolation'] ); ?>> <?php esc_html_e( 'Изоляция страниц (COOP/COEP): распознавание в 2–3 раза быстрее', 'giga-pisar' ); ?></label>
					<p class="description"><?php esc_html_e( 'Страницы, где работает диктовка, получают заголовки Cross-Origin-Opener-Policy и Cross-Origin-Embedder-Policy: credentialless. Может сломать всплывающие окна входа через соцсети и некоторые встраивания. Включайте и проверяйте сайт.', 'giga-pisar' ); ?></p>
				</td></tr>
			<?php else : ?>
				<tr><th scope="row"><?php esc_html_e( 'Мозг Писаря', 'giga-pisar' ); ?></th><td>
					<label><input type="checkbox" name="<?php echo esc_attr( $name( 'brain' ) ); ?>" value="1" <?php checked( $o['brain'] ); ?>> <?php esc_html_e( 'Включить очистку речи и команды Писарю', 'giga-pisar' ); ?></label>
					<p class="description"><?php esc_html_e( 'Нейронка убирает слова-паразиты и оговорки, исправляет ошибки распознавания. Команды: «…, Писарь, исправь» (сократи, переведи на английский) в конце диктовки; выделить текст и продиктовать команду; кнопки «Причесать», «Сократить», «Перевести». Выключено — диктовка вставляет текст как распознан.', 'giga-pisar' ); ?></p>
				</td></tr>
				<tr><th scope="row"><?php esc_html_e( 'Нейронка', 'giga-pisar' ); ?></th><td>
					<label><input type="radio" name="<?php echo esc_attr( $name( 'brain_provider' ) ); ?>" value="qwen" <?php checked( $o['brain_provider'], 'qwen' ); ?>> <strong>Qwen3-4B</strong> — <?php esc_html_e( 'в браузере человека: при первом использовании он соглашается скачать 1,9 ГБ; нужен компьютер с 8 ГБ памяти', 'giga-pisar' ); ?></label><br>
					<label><input type="radio" name="<?php echo esc_attr( $name( 'brain_provider' ) ); ?>" value="gigachat" <?php checked( $o['brain_provider'], 'gigachat' ); ?>> <strong>GigaChat</strong> — <?php esc_html_e( 'на сервере (llama-server): родной русский; на сервер уходит только текст', 'giga-pisar' ); ?></label>
				</td></tr>
				<tr><th scope="row"><label for="gp-gigachat"><?php esc_html_e( 'Адрес GigaChat', 'giga-pisar' ); ?></label></th><td>
					<input id="gp-gigachat" class="regular-text code" type="url" name="<?php echo esc_attr( $name( 'gigachat_url' ) ); ?>" value="<?php echo esc_attr( $o['gigachat_url'] ); ?>" placeholder="http://127.0.0.1:8091/">
					<?php if ( $o['gigachat_url'] ) : ?>
						<?php $h = giga_pisar_gigachat_health( true ); ?>
						<p><?php esc_html_e( 'Сейчас:', 'giga-pisar' ); ?> <strong><?php echo esc_html( array( 'ok' => __( 'отвечает', 'giga-pisar' ), 'loading' => __( 'загружает модель', 'giga-pisar' ), 'absent' => __( 'не отвечает', 'giga-pisar' ) )[ $h ] ); ?></strong></p>
					<?php endif; ?>
					<p class="description"><?php esc_html_e( 'OpenAI-совместимый llama-server с моделью GigaChat3.1-10B-A1.8B (см. web/deploy в репозитории). Браузеры к нему не ходят — запросы идут через WordPress с проверкой прав и лимитом 12 правок в минуту на человека.', 'giga-pisar' ); ?></p>
				</td></tr>
				<tr><th scope="row"><?php esc_html_e( 'Кому доступен мозг', 'giga-pisar' ); ?></th><td>
					<label><input type="radio" name="<?php echo esc_attr( $name( 'brain_who' ) ); ?>" value="admins" <?php checked( $o['brain_who'], 'admins' ); ?>> <?php esc_html_e( 'Только администраторам', 'giga-pisar' ); ?></label><br>
					<label><input type="radio" name="<?php echo esc_attr( $name( 'brain_who' ) ); ?>" value="dictation" <?php checked( $o['brain_who'], 'dictation' ); ?>> <?php esc_html_e( 'Всем, кто может диктовать', 'giga-pisar' ); ?></label>
					<p class="description"><?php esc_html_e( 'Остальные пользуются прямой диктовкой: текст вставляется как распознан. Включить или выключить мозг может только администратор.', 'giga-pisar' ); ?></p>
				</td></tr>
				<tr><th scope="row"><?php esc_html_e( 'Мозг на компьютере пользователя', 'giga-pisar' ); ?></th><td>
					<label><input type="checkbox" name="<?php echo esc_attr( $name( 'brain_local' ) ); ?>" value="1" <?php checked( $o['brain_local'] ); ?>> <?php esc_html_e( 'Разрешить пользователю подключить свою нейронку', 'giga-pisar' ); ?></label>
					<p class="description"><?php esc_html_e( 'Человек ставит у себя программу с нейронкой (GigaBrain.exe из репозитория — один файл, сам всё скачает; или Ollama, LM Studio) и в подсказке Писаря (⚙) указывает её адрес. Страница ходит к ней по http://127.0.0.1 напрямую, текст на сайт не отправляется. Любая модель, которую он туда поставит; на компьютере с 32 ГБ памяти — GigaChat, Qwen3-14B, YandexGPT и т. п. — быстрее, чем на сервере.', 'giga-pisar' ); ?></p>
				</td></tr>
				<tr><th scope="row"><?php esc_html_e( 'Функции на кнопках', 'giga-pisar' ); ?></th><td>
					<?php $chips = (array) $o['chips']; ?>
					<table class="widefat" id="gp-chips" style="max-width:820px">
						<thead><tr><th style="width:26%"><?php esc_html_e( 'Кнопка', 'giga-pisar' ); ?></th><th><?php esc_html_e( 'Команда нейронке', 'giga-pisar' ); ?></th><th style="width:40px"></th></tr></thead>
						<tbody>
						<?php for ( $i = 0; $i < GIGA_PISAR_MAX_CHIPS; $i++ ) : ?>
							<?php $c = $chips[ $i ] ?? array( 'title' => '', 'command' => '' ); ?>
							<tr <?php echo $i >= count( $chips ) && $i > 0 ? 'class="gp-empty"' : ''; ?>>
								<td><input type="text" class="widefat" maxlength="40" name="<?php echo esc_attr( $name( 'chips' ) ); ?>[<?php echo (int) $i; ?>][title]" value="<?php echo esc_attr( $c['title'] ); ?>" placeholder="<?php esc_attr_e( 'Название', 'giga-pisar' ); ?>"></td>
								<td><textarea class="widefat" rows="2" maxlength="300" name="<?php echo esc_attr( $name( 'chips' ) ); ?>[<?php echo (int) $i; ?>][command]" placeholder="<?php esc_attr_e( 'что сделать с текстом, как сказали бы вслух', 'giga-pisar' ); ?>"><?php echo esc_textarea( $c['command'] ); ?></textarea></td>
								<td><button type="button" class="button-link gp-clear" title="<?php esc_attr_e( 'Очистить', 'giga-pisar' ); ?>">✕</button></td>
							</tr>
						<?php endfor; ?>
						</tbody>
					</table>
					<p>
						<button type="button" class="button" id="gp-chips-add"><?php esc_html_e( 'Добавить кнопку', 'giga-pisar' ); ?></button>
						<button type="submit" class="button" name="<?php echo esc_attr( $name( 'chips_reset' ) ); ?>" value="1" onclick="return confirm('<?php echo esc_js( __( 'Вернуть 7 кнопок по умолчанию? Свои правки пропадут.', 'giga-pisar' ) ); ?>')"><?php esc_html_e( 'Вернуть по умолчанию', 'giga-pisar' ); ?></button>
					</p>
					<p class="description"><?php printf( esc_html__( 'До %d кнопок. Строки без названия или команды не сохраняются. Команда идёт нейронке в конце промпта как «Команда пользователя к тексту: …». Кнопки появляются после диктовки и работают над выделенным, а без выделения — над всем текстом поля. Голосом можно сказать любую команду: «…, Писарь, сделай список».', 'giga-pisar' ), (int) GIGA_PISAR_MAX_CHIPS ); ?></p>
					<script>
					( function () {
						const rows = [ ...document.querySelectorAll( '#gp-chips tbody tr' ) ];
						const refresh = () => {
							let shown = 0;
							rows.forEach( ( r ) => {
								const filled = [ ...r.querySelectorAll( 'input, textarea' ) ].some( ( f ) => f.value.trim() );
								if ( filled || shown === 0 ) { r.style.display = ''; shown++; } else if ( ! r.dataset.open ) { r.style.display = 'none'; } else { shown++; }
							} );
							document.getElementById( 'gp-chips-add' ).disabled = shown >= rows.length;
						};
						document.getElementById( 'gp-chips-add' ).addEventListener( 'click', () => {
							const r = rows.find( ( x ) => x.style.display === 'none' );
							if ( r ) { r.dataset.open = '1'; r.style.display = ''; r.querySelector( 'input' ).focus(); refresh(); }
						} );
						rows.forEach( ( r ) => r.querySelector( '.gp-clear' ).addEventListener( 'click', () => {
							r.querySelectorAll( 'input, textarea' ).forEach( ( f ) => { f.value = ''; } );
							delete r.dataset.open;
							refresh();
						} ) );
						refresh();
					}() );
					</script>
				</td></tr>
				<?php $defs = giga_pisar_default_prompts(); ?>
				<tr><th scope="row"><label for="gp-prompt-d"><?php esc_html_e( 'Промпт для диктовки', 'giga-pisar' ); ?></label></th><td>
					<textarea id="gp-prompt-d" class="large-text" rows="6" name="<?php echo esc_attr( $name( 'prompt_dictation' ) ); ?>"><?php echo esc_textarea( giga_pisar_prompt( 'dictation' ) ); ?></textarea>
					<p class="description"><?php esc_html_e( 'Системная инструкция для «…, Писарь, команда» в конце диктовки. К ней в конце дописывается «Команда пользователя к тексту: …». Пустое поле или образец без изменений = вшитый промпт.', 'giga-pisar' ); ?>
						<button type="button" class="button-link" onclick="document.getElementById('gp-prompt-d').value=<?php echo esc_attr( wp_json_encode( $defs['dictation'] ) ); ?>"><?php esc_html_e( 'Вернуть образец', 'giga-pisar' ); ?></button></p>
				</td></tr>
				<tr><th scope="row"><label for="gp-prompt-s"><?php esc_html_e( 'Промпт для выделенного и кнопок', 'giga-pisar' ); ?></label></th><td>
					<textarea id="gp-prompt-s" class="large-text" rows="6" name="<?php echo esc_attr( $name( 'prompt_selection' ) ); ?>"><?php echo esc_textarea( giga_pisar_prompt( 'selection' ) ); ?></textarea>
					<p class="description"><?php esc_html_e( 'Для команды над выделенным текстом и для кнопок. Здесь удобно задать стиль издания: «пиши по нормам такого-то издания», «обращение на вы» и т. п.', 'giga-pisar' ); ?>
						<button type="button" class="button-link" onclick="document.getElementById('gp-prompt-s').value=<?php echo esc_attr( wp_json_encode( $defs['selection'] ) ); ?>"><?php esc_html_e( 'Вернуть образец', 'giga-pisar' ); ?></button></p>
				</td></tr>
			<?php endif; ?>
			</table>
			<?php submit_button(); ?>
		</form>
		<?php endif; ?>
	</div>
	<?php
}

function giga_pisar_models_tab() {
	$o = giga_pisar_options();
	?>
	<p><?php esc_html_e( 'Браузеры посетителей не могут скачать модели прямо с GitHub. Выложите копию на свой сервер — одной кнопкой (сервер скачает сам) или руками в папку:', 'giga-pisar' ); ?>
		<code><?php echo esc_html( giga_pisar_models_dir() ); ?></code></p>
	<table class="widefat striped" style="max-width:900px">
		<thead><tr><th><?php esc_html_e( 'Модель', 'giga-pisar' ); ?></th><th><?php esc_html_e( 'На сервере', 'giga-pisar' ); ?></th><th></th></tr></thead>
		<tbody>
		<?php foreach ( giga_pisar_model_catalog() as $key => $m ) : ?>
			<?php $size = giga_pisar_model_size( $key ); ?>
			<tr>
				<td><strong><?php echo esc_html( $m[2] ); ?></strong><br><code><?php echo esc_html( $m[0] ); ?></code></td>
				<td><?php echo esc_html( giga_pisar_size_text( $size ) ); ?></td>
				<td>
					<form method="post" action="<?php echo esc_url( admin_url( 'admin-post.php' ) ); ?>" style="display:inline">
						<?php wp_nonce_field( 'giga_pisar_models' ); ?>
						<input type="hidden" name="model" value="<?php echo esc_attr( $key ); ?>">
						<?php if ( $size ) : ?>
							<input type="hidden" name="action" value="giga_pisar_delete_model">
							<?php submit_button( __( 'Удалить с сервера', 'giga-pisar' ), 'delete small', 'submit', false ); ?>
						<?php else : ?>
							<input type="hidden" name="action" value="giga_pisar_fetch_model">
							<?php submit_button( __( 'Скачать на сервер', 'giga-pisar' ), 'secondary small', 'submit', false, array( 'onclick' => "this.value='" . esc_js( __( 'Качаю… не закрывайте вкладку', 'giga-pisar' ) ) . "'" ) ); ?>
						<?php endif; ?>
					</form>
				</td>
			</tr>
		<?php endforeach; ?>
		</tbody>
	</table>

	<h2><?php esc_html_e( 'Свои адреса', 'giga-pisar' ); ?></h2>
	<form method="post" action="options.php">
		<?php settings_fields( 'giga_pisar' ); ?>
		<input type="hidden" name="giga_pisar_options[_tab]" value="models">
		<table class="form-table" role="presentation">
			<tr><th scope="row"><label for="gp-model-url"><?php esc_html_e( 'Архив GigaAM', 'giga-pisar' ); ?></label></th><td>
				<input id="gp-model-url" class="regular-text code" type="url" name="giga_pisar_options[model_url]" value="<?php echo esc_attr( $o['model_url'] ); ?>">
				<p class="description"><?php esc_html_e( 'Если копии на сервере нет. Адрес должен отдаваться браузеру с разрешением CORS.', 'giga-pisar' ); ?></p>
			</td></tr>
			<tr><th scope="row"><label for="gp-qwen-url"><?php esc_html_e( 'Qwen .gguf', 'giga-pisar' ); ?></label></th><td>
				<input id="gp-qwen-url" class="regular-text code" type="url" name="giga_pisar_options[qwen_url]" value="<?php echo esc_attr( $o['qwen_url'] ); ?>">
				<p class="description"><?php esc_html_e( 'Без копии на сервере и своего адреса Qwen качается с Hugging Face.', 'giga-pisar' ); ?></p>
			</td></tr>
		</table>
		<?php submit_button(); ?>
	</form>
	<?php
}

/** «Проверка в этом браузере»: что мешает диктовке прямо здесь и сейчас. */
function giga_pisar_diagnostics_box() {
	$enabled = giga_pisar_can_dictate( 'admin' );
	?>
	<div class="card" style="max-width:900px;margin-top:16px">
		<h2 class="title"><?php esc_html_e( 'Проверка в этом браузере', 'giga-pisar' ); ?></h2>
		<?php if ( ! $enabled ) : ?>
			<p><?php esc_html_e( 'Диктовка в админке выключена — проверка работает, когда включена галочка «В админке».', 'giga-pisar' ); ?></p>
		<?php else : ?>
			<table class="widefat striped" id="giga-pisar-diag"><tbody>
				<tr><td><?php esc_html_e( 'Проверяю…', 'giga-pisar' ); ?></td></tr>
			</tbody></table>
			<p class="description"><?php esc_html_e( 'Где кнопки: плавающий микрофон — у края поля, где стоит курсор; в блочном редакторе — значок микрофона на панели выбранного блока с текстом; в классическом — на панели форматирования.', 'giga-pisar' ); ?></p>
			<script>
			( function () {
				const tbody = document.querySelector( '#giga-pisar-diag tbody' );
				const esc = ( s ) => String( s ).replace( /[&<>"]/g, ( c ) => ( { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[ c ] ) );
				const show = ( rows ) => {
					tbody.innerHTML = rows.map( ( r ) => '<tr><td style="width:24px">' + ( r.ok ? '✅' : '❌' ) + '</td><td><strong>' + esc( r.name ) + '</strong></td><td>' + esc( r.detail || '' ) + '</td></tr>' ).join( '' );
				};
				const run = () => window.GigaPisar.diagnose().then( show );
				if ( window.GigaPisar ) {
					run();
				} else {
					document.addEventListener( 'giga-pisar-ready', run, { once: true } );
					setTimeout( () => {
						if ( ! window.GigaPisar ) {
							show( [ { ok: false, name: <?php echo wp_json_encode( __( 'Скрипт плагина не загрузился', 'giga-pisar' ) ); ?>, detail: <?php echo wp_json_encode( __( 'Скорее всего, его склеил или отложил плагин оптимизации (LiteSpeed, WP Rocket, Autoptimize) или Cloudflare Rocket Loader — исключите giga-pisar из оптимизации JS. Подробности — в консоли браузера (F12).', 'giga-pisar' ) ); ?> } ] );
						}
					}, 10000 );
				}
			}() );
			</script>
		<?php endif; ?>
	</div>
	<?php
}
