/**
 * Кнопка «Диктовка» в панели форматирования блочного редактора (Gutenberg).
 * Текст встаёт в блок туда, где курсор; с мозгом — команды над выделенным.
 * Сама запись и распознавание — в pisar-wp.js (window.GigaPisar).
 */
( function ( wp ) {
	const { registerFormatType, insert } = wp.richText;
	const { RichTextToolbarButton } = wp.blockEditor;
	const { createElement: h, useRef, useEffect, useState } = wp.element;

	// По-русски, если русский у сайта или у браузера человека
	const ru = [ document.documentElement.lang, navigator.language ].some( ( l ) => /^ru/i.test( l || '' ) );
	const L = ( r, e ) => ( ru ? r : e );

	/** window.GigaPisar появляется, когда догрузится модуль. */
	function whenReady( fn ) {
		if ( window.GigaPisar ) {
			fn( window.GigaPisar );
		} else {
			document.addEventListener( 'giga-pisar-ready', () => fn( window.GigaPisar ), { once: true } );
		}
	}

	function Edit( props ) {
		// Запись длится секунды, за это время блок перерисуется: берём свежие value/onChange.
		const latest = useRef( props );
		latest.current = props;
		const [ recording, setRecording ] = useState( false );

		useEffect( () => {
			const onState = ( e ) => setRecording( e.detail.state === 'recording' && e.detail.target && e.detail.target.kind === 'block' );
			document.addEventListener( 'giga-pisar-state', onState );
			return () => document.removeEventListener( 'giga-pisar-state', onState );
		}, [] );

		const adapter = {
			kind: 'block',
			anchor: null,
			selection() {
				const v = latest.current.value;
				const text = v.text.slice( v.start, v.end );
				return { text, empty: ! text };
			},
			insert( text ) {
				const v = latest.current.value;
				let t = text;
				const before = v.text[ v.start - 1 ];
				const after = v.text[ v.end ];
				if ( before && ! /\s/.test( before ) ) {
					t = ' ' + t;
				}
				if ( after && ! /[\s.,!?;:…)\]»"']/.test( after ) ) {
					t += ' ';
				}
				latest.current.onChange( insert( v, t ) );
			},
			replace( text ) {
				const v = latest.current.value;
				latest.current.onChange( insert( v, text, v.start, v.end ) );
			},
			wholeText: () => null, // форматирование блока не трогаем: только выделенное
			undo() {
				const editor = wp.data.dispatch( 'core/editor' );
				if ( editor && editor.undo ) {
					editor.undo();
					return true;
				}
				return false;
			},
		};

		return h( RichTextToolbarButton, {
			icon: 'microphone',
			title: recording ? L( 'Остановить диктовку', 'Stop dictation' ) : L( 'Диктовка', 'Dictation' ),
			isActive: recording,
			onClick( event ) {
				const anchor = event && event.currentTarget;
				whenReady( ( gp ) => gp.toggle( adapter, anchor ) );
			},
		} );
	}

	registerFormatType( 'giga-pisar/dictate', {
		title: L( 'Диктовка', 'Dictation' ),
		tagName: 'span',
		className: 'giga-pisar-dictate',
		edit: Edit,
	} );
}( window.wp ) );
