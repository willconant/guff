'use strict';

var article = null;
var unsavedChanges = false;

$(function() {
	var $buttons = $('div.edit-buttons');

	$buttons.on('click', '.js-login', function(e) { e.preventDefault(); navigator.id.request() });
	$buttons.on('click', '.js-logout', function(e) { e.preventDefault(); navigator.id.logout() });
	$buttons.on('click', '.js-edit-article', editArticle);
	$buttons.on('click', '.js-save-article', saveArticle);

	if (auth.email === '') {
		$buttons.find('.js-login').show();
	}
	else {
		$buttons.find('.js-logout').show();
	}

	if (auth.write) {
		$buttons.find('.js-edit-article').show();
	}

	if (auth.admin) {
		$buttons.find('.js-admin').show();
	}

	// var left = $('#sidebar').offset().left;
	// $('#sidebar').css('position', 'fixed').css('left', left);;
	
	window.onbeforeunload = function() {
		if (unsavedChanges) {
			return 'You have unsaved changes.';
		}
	};

	navigator.id.watch({
		loggedInUser: auth.email || null,
		onlogin: function(assertion) {
			$.ajax({
				type: 'POST',
				url: '/_login',
				data: { assertion: assertion },
				success: function(res, status, xhr) { window.location.reload(); },
				error: function(res, status, xhr) { alert('login failure' + res); }
			});
		},
		onlogout: function() {
			$.ajax({
				type: 'POST',
				url: '/_logout',
				success: function(res, status, xhr) { window.location = '/'; },
				error: function(res, status, xhr) { alert('logout failure' + res); }
			});
		}
	})
});

function editArticle(e) {
	e.preventDefault();
	
	var $modal = $('<div class="modal">');
	var $title = $('<input type="text">').appendTo($('<div class="title">').appendTo($modal));
	var $editor = $('<textarea class="editor"></textarea>').appendTo($modal);

	if (article !== null) {
		onGet(article);
	}
	else {
		$.ajax({
			type:     'GET',
			url:      window.location.pathname + '?json=1',
			success:  onGet,
			error:    onAjaxError,
			dataType: 'json'
		});
	}
	
	function onGet(result) {
		article = result;
		
		$('.js-edit-article').hide();
		$('.js-save-article').hide();
		$('.js-view-article').show().unbind('click').click(function(e) {
			e.preventDefault();
			
			$.ajax({
				type:     'POST',
				url:      '/_markdown',
				data:     $editor.val(),
				success:  onMarkdown,
				error:    onAjaxError,
				dataType: 'text'
			});
		});
		
		$('article .js-article-body').html('');
		$modal.appendTo('body');
		
		$title.val(article.Title);
		$editor.val(article.Markdown);
		$editor.height($(window).height() - 135);
		$modal.height($(window).height());
		$('.visibility').show();
	}
	
	function onMarkdown(html, status) {
		var newTitle = $title.val() || article._id;
		var newMarkdown = $editor.val();
		var newPublic = $('#js-is-public')[0].checked;

		if (newTitle !== article.Title) {
			article.Title = newTitle;
			unsavedChanges = true;
		}
		if (newMarkdown !== article.Markdown) {
			article.Markdown = newMarkdown;
			unsavedChanges = true;
		}
		if (newPublic !== article.Public) {
			article.Public = newPublic;
			unsavedChanges = true;
		}
		
		$('article h1.primary span.content').text(article.Title);
		$('article .js-article-body').html(html);
		
		$modal.remove();
		$('.js-edit-article').show();
		$('.js-view-article').hide();
		$('.visibility').hide();
		
		if (unsavedChanges) {
			$('.js-save-article').show();
		}
	}
}

function setPublic(isPublic) {
	if (article !== null) {
		onGet(article);
	}
	else {
		$.ajax({
			type:     'GET',
			url:      window.location.pathname + '?json=1',
			success:  onGet,
			error:    onAjaxError,
			dataType: 'json'
		});
	}

	function onGet(result) {
		article = result;
		if (article.Public !== isPublic) {
			article.Public = isPublic;
			unsavedChanges = true;
			$('.js-save-article').show();
		}
	}
}

function saveArticle(e) {
	e.preventDefault();
	
	if (article === null) {
		return;
	}
	
	$.ajax({
		type:     'PUT',
		url:      window.location.href,
		data:     article,
		success:  onPut,
		error:    onAjaxError,
		dataType: 'json'
	});
	
	function onPut(response) {
		if (response.conflict) {
			alert('There was a conflict saving this article.');
		}
		else {
			article._rev = response.rev;
			$('.js-save-article').hide();
			$('.js-saved-message').show();
			unsavedChanges = false;
			//setTimeout(function(){ $('.js-saved-message').fadeOut('slow'); }, 250);
			setTimeout(function() { window.location.reload(); }, 500);
		}
	}
}

function onAjaxError(xhr, status, error) {
	alert(error);
}
