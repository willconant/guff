var article = null;

$(function() {
	var $buttons = $('<div class="edit-buttons">').appendTo('h1.primary');
	$('<a href="#" class="js-edit-title">[Edit Title] </a>').appendTo($buttons);
	$('<a href="#" class="js-edit-markdown">[Edit Body] </a>').appendTo($buttons);
	$('<a href="#" class="js-save-changes disabled">[Save Changes] </a>').appendTo($buttons);
	$('<span class="js-saved-message"> <-- Your changes have been saved.</span>').appendTo($buttons).hide();

	$('article').on('click', 'h1.primary .js-edit-title', editArticleTitle);
	$('article').on('click', 'h1.primary .js-edit-markdown', editArticleMarkdown);
	$('article').on('click', 'h1.primary .js-save-changes', putArticle);
});

function editArticleTitle(e) {
	e.preventDefault();
	var $content = $('h1.primary span.content');
	
	if (article !== null) {
		onGet(article);
	}
	else {
		$.ajax({
			type:     'GET',
			url:      window.location.href + '?json=1',
			success:  onGet,
			dataType: 'json'
		});
	}
	
	function onGet(result) {
		article = result;
		
		$('article .js-save-changes').removeClass('disabled');
		
		article.Title = prompt("Title", article.Title);
		$content.text(article.Title);
	}
}

function editArticleMarkdown(e) {
	e.preventDefault();
	
	var $content = $('div.js-article-body');
	var $modal = $('<div class="modal">');
	var $editor = $('<textarea class="editor"></textarea>').appendTo($modal);
	var $buttons = $('<div class="buttons">').appendTo($modal);
	var $save = $('<a class="js-save-button">[Preview]</a>').appendTo($buttons);
	
	if (article !== null) {
		onGet(article);
	}
	else {
		$.ajax({
			type:     'GET',
			url:      window.location.href + '?json=1',
			success:  onGet,
			dataType: 'json'
		});
	}
	
	function onGet(result) {
		article = result;
		
		$('article .js-save-changes').removeClass('disabled');
		
		$save.click(function() {
			$.ajax({
				type:     'POST',
				url:      '/_markdown',
				data:     $editor.val(),
				success:  onSave,
				dataType: 'text'
			});
		});
		
		$modal.appendTo('body');
		
		$editor.val(article.Markdown);
		$editor.width($(window).width() - 100);
		$editor.height($(window).height() - 150);
		$editor[0].focus();
	}
	
	function onSave(html, status) {
		article.Markdown = $editor.val();
		$content[0].innerHTML = html;
		$modal.remove();
	}
}

function putArticle(e) {
	e.preventDefault();
	
	if (article === null) {
		return;
	}
	
	$.ajax({
		type:     'PUT',
		url:      window.location.href,
		data:     article,
		success:  onPut,
		error:    onError,
		dataType: 'json'
	});
	
	function onPut(response) {
		if (response.conflict) {
			alert("There was a conflict saving this article.");
		}
		else {
			article._rev = response.rev;
			$('.js-saved-message').show();
			setTimeout(function(){ $('.js-saved-message').fadeOut('slow'); }, 1000);
		}
	}
	
	function onError(xhr, status, error) {
		alert(error);
	}
}
