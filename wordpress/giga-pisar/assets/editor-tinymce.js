/**
 * Кнопка «Диктовка» в классическом редакторе (TinyMCE).
 * Сама запись и распознавание — в pisar-wp.js (window.GigaPisar).
 */
/* global tinymce */
( function () {
	// По-русски, если русский у сайта или у браузера человека
	const ru = [ document.documentElement.lang, navigator.language ].some( ( l ) => /^ru/i.test( l || '' ) );
	const L = ( r, e ) => ( ru ? r : e );

	const escape = ( s ) => s.replace( /&/g, '&amp;' ).replace( /</g, '&lt;' ).replace( />/g, '&gt;' );

	function whenReady( fn ) {
		if ( window.GigaPisar ) {
			fn( window.GigaPisar );
		} else {
			document.addEventListener( 'giga-pisar-ready', () => fn( window.GigaPisar ), { once: true } );
		}
	}

	tinymce.PluginManager.add( 'giga_pisar', function ( editor ) {
		let bookmark = null;

		const adapter = {
			kind: 'tinymce',
			anchor: null,
			remember() {
				bookmark = editor.selection.getBookmark( 2, true );
			},
			selection() {
				const text = editor.selection.getContent( { format: 'text' } );
				return { text, empty: editor.selection.isCollapsed() || ! text };
			},
			insert( text ) {
				editor.focus();
				if ( bookmark ) {
					editor.selection.moveToBookmark( bookmark );
				}
				let t = text;
				const rng = editor.selection.getRng();
				const node = rng.startContainer;
				if ( node && node.nodeType === 3 && rng.startOffset > 0 && ! /\s/.test( node.data[ rng.startOffset - 1 ] ) ) {
					t = ' ' + t;
				}
				editor.insertContent( escape( t ) );
				bookmark = null;
			},
			replace( text ) {
				editor.focus();
				if ( bookmark ) {
					editor.selection.moveToBookmark( bookmark );
				}
				editor.undoManager.transact( () => editor.selection.setContent( escape( text ) ) );
				bookmark = null;
			},
			wholeText: () => null, // разметку не трогаем: только выделенное
			undo() {
				editor.undoManager.undo();
				return true;
			},
		};

		editor.addButton( 'giga_pisar', {
			title: L( 'Диктовка', 'Dictation' ),
			icon: 'dashicon dashicons-microphone',
			onPostRender() {
				const btn = this;
				document.addEventListener( 'giga-pisar-state', ( e ) => {
					btn.active( e.detail.state === 'recording' && e.detail.target === adapter );
				} );
			},
			onclick() {
				adapter.anchor = this.getEl();
				whenReady( ( gp ) => gp.toggle( adapter, adapter.anchor ) );
			},
		} );
	} );
}() );
