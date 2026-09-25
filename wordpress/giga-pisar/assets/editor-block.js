/**
 * Кнопка микрофона на панели блока в блочном редакторе (Gutenberg).
 *
 * Кнопка стоит прямо на панели инструментов у любого блока с текстом
 * (абзац, заголовок, список, цитата…), а не в меню «Ещё». Текст встаёт
 * туда, где курсор; с мозгом — команды над выделенным.
 * Сама запись и распознавание — в pisar-wp.js (window.GigaPisar).
 */
( function ( wp ) {
	const { create, insert, toHTMLString } = wp.richText;
	const { BlockControls } = wp.blockEditor;
	const { ToolbarGroup, ToolbarButton } = wp.components;
	const { createElement: h, Fragment, useEffect, useState } = wp.element;
	const { createHigherOrderComponent } = wp.compose;
	const { select, dispatch } = wp.data;

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

	/** Текстовые атрибуты блока (те, что редактируются через RichText). */
	function richAttributes( blockName ) {
		const type = wp.blocks.getBlockType( blockName );
		if ( ! type || ! type.attributes ) {
			return [];
		}
		return Object.keys( type.attributes ).filter( ( k ) => {
			const a = type.attributes[ k ];
			return a.source === 'rich-text' || a.type === 'rich-text' || ( a.source === 'html' && a.selector );
		} );
	}

	/**
	 * Куда вставлять: блок, его текстовый атрибут и границы выделения.
	 * Берётся из хранилища редактора, поэтому работает и когда холст
	 * редактора — iframe, и после того, как фокус ушёл на кнопку.
	 */
	function caret( clientId ) {
		const be = select( 'core/block-editor' );
		const s = be.getSelectionStart();
		const e = be.getSelectionEnd();
		const block = be.getBlock( clientId );
		if ( ! block ) {
			return null;
		}
		let key = s && s.clientId === clientId ? s.attributeKey : null;
		if ( ! key ) {
			key = richAttributes( block.name )[ 0 ];
		}
		if ( ! key ) {
			return null;
		}
		const value = create( { html: String( block.attributes[ key ] ?? '' ) } );
		let start = value.text.length;
		let end = start;
		if ( s && s.clientId === clientId && s.attributeKey === key && typeof s.offset === 'number' ) {
			start = s.offset;
			end = e && e.clientId === clientId && e.attributeKey === key && typeof e.offset === 'number' ? e.offset : s.offset;
			if ( end < start ) {
				[ start, end ] = [ end, start ];
			}
		}
		return { clientId, key, value, start, end };
	}

	function write( c, text, start, end ) {
		const next = insert( c.value, text, start, end );
		dispatch( 'core/block-editor' ).updateBlockAttributes( c.clientId, { [ c.key ]: toHTMLString( { value: next } ) } );
		const at = start + text.length;
		dispatch( 'core/block-editor' ).selectionChange( c.clientId, c.key, at, at );
	}

	function makeAdapter( clientId ) {
		let saved = null; // выделение в момент нажатия — запись длится секунды
		const current = () => caret( clientId ) || saved;
		return {
			kind: 'block',
			clientId,
			anchor: null,
			remember() {
				saved = caret( clientId );
			},
			selection() {
				const c = current();
				const text = c ? c.value.text.slice( c.start, c.end ) : '';
				return { text, empty: ! text };
			},
			insert( text ) {
				const c = current();
				if ( ! c ) {
					return;
				}
				let t = text;
				const before = c.value.text[ c.start - 1 ];
				const after = c.value.text[ c.end ];
				if ( before && ! /\s/.test( before ) ) {
					t = ' ' + t;
				}
				if ( after && ! /[\s.,!?;:…)\]»"']/.test( after ) ) {
					t += ' ';
				}
				write( c, t, c.start, c.end );
			},
			replace( text ) {
				const c = current();
				if ( c ) {
					write( c, text, c.start, c.end );
				}
			},
			wholeText: () => null, // форматирование блока не трогаем: только выделенное
			undo() {
				const editor = dispatch( 'core/editor' );
				if ( editor && editor.undo ) {
					editor.undo();
					return true;
				}
				return false;
			},
		};
	}

	function MicButton( { clientId } ) {
		const [ state, setState ] = useState( 'idle' );
		useEffect( () => {
			const onState = ( e ) => {
				const mine = e.detail.target && e.detail.target.kind === 'block' && e.detail.target.clientId === clientId;
				setState( mine ? e.detail.state : 'idle' );
			};
			document.addEventListener( 'giga-pisar-state', onState );
			return () => document.removeEventListener( 'giga-pisar-state', onState );
		}, [ clientId ] );

		const recording = state === 'recording';
		return h(
			BlockControls,
			{ group: 'other' },
			h(
				ToolbarGroup,
				null,
				h( ToolbarButton, {
					icon: 'microphone',
					label: recording ? L( 'Остановить диктовку', 'Stop dictation' ) : L( 'Диктовка', 'Dictation' ),
					isPressed: recording,
					isBusy: state === 'busy' || state === 'starting',
					onClick( event ) {
						const anchor = event && event.currentTarget;
						const adapter = makeAdapter( clientId );
						adapter.remember();
						whenReady( ( gp ) => gp.toggle( adapter, anchor ) );
					},
				} )
			)
		);
	}

	const withMic = createHigherOrderComponent( ( BlockEdit ) => ( props ) => {
		if ( ! props.isSelected || ! richAttributes( props.name ).length ) {
			return h( BlockEdit, props );
		}
		return h( Fragment, null, h( BlockEdit, props ), h( MicButton, { clientId: props.clientId } ) );
	}, 'withGigaPisarMic' );

	wp.hooks.addFilter( 'editor.BlockEdit', 'giga-pisar/mic', withMic );

	// Плавающий микрофон у блока, где стоит курсор — даже у пустого абзаца,
	// у которого WordPress панель блока не показывает. Холст редактора
	// бывает в iframe: элемент блока ищем там.
	function canvasDocument() {
		const frame = document.querySelector( 'iframe[name="editor-canvas"]' );
		return ( frame && frame.contentDocument ) || document;
	}

	let floatingFor = null;
	wp.data.subscribe( () => {
		const be = select( 'core/block-editor' );
		const id = be && be.getSelectedBlockClientId();
		const block = id && be.getBlock( id );
		const key = block && richAttributes( block.name ).length ? id : null;
		if ( key === floatingFor ) {
			return;
		}
		floatingFor = key;
		whenReady( ( gp ) => {
			if ( ! key ) {
				gp.float( null );
				return;
			}
			// DOM блока появляется после отрисовки
			requestAnimationFrame( () => {
				if ( floatingFor !== key ) {
					return;
				}
				const el = canvasDocument().querySelector( '[data-block="' + key + '"]' );
				gp.float( el ? { el, adapter: makeAdapter( key ) } : null );
			} );
		} );
	} );
}( window.wp ) );
