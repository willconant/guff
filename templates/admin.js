'use strict';

$(function() {
	$('.user-row').each(function(i, rowElt) {
		var startRole = $(rowElt).attr('data-start-role');
		$(rowElt).find('.role-radio[value=' + startRole + ']')[0].checked = true;
	});

	$('.user-row .role-radio').click(function() {
		var email = $(this).attr('name').split('-', 2)[1];
		var startRole = $(this).parents('.user-row').attr('data-start-role');
		if (auth.email === email) {
			alert("You cannot change your own role.");
			$(this).parents('.user-row').find('.role-radio[value=' + startRole + ']')[0].checked = true;
			return;
		}
		var newRole = $(this).val();
		updateUserRole(email, newRole);
	});
});

function updateUserRole(email, role) {	
	$.ajax({
		type:     'POST',
		url:      '/_admin/role',
		data:     {Email: email, Role: role},
		success:  onPost,
		error:    onAjaxError,
		dataType: 'json'
	});
	
	function onPost(response) {
		if (!response.ok) {
			alert(response.error);
		}
	}
}

function onAjaxError(xhr, status, error) {
	alert(error);
}
