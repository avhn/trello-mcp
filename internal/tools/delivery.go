package tools

const cardCreateParams = "name desc due start pos idMembers idLabels checklist"

// deliveryDefinitions extends the existing reads with writes and checklist item search.
func (s *Service) deliveryDefinitions() []definition {
	return []definition{
		{name: "board_create", description: "Create a board in an authorized workspace.", params: "workspaceId name desc defaultLists", required: "name", write: true, call: s.boardCreate},
		{name: "list_create", description: "Create a board list.", params: "boardId name pos", required: "name", write: true, call: s.listCreate},
		{name: "list_update", description: "Rename, reorder, move or reopen a list; both boards checked.", params: "boardId listId name pos idBoard closed", required: "listId", write: true, call: s.listUpdate},
		{name: "list_archive", description: "Archive a list.", params: "boardId listId", required: "listId", write: true, call: s.listArchive},
		{name: "list_watch", description: "Set list subscription.", params: "boardId listId subscribed", required: "listId", write: true, call: s.listWatch},
		{name: "card_create", description: "Create a card; optional checklist with ordered items. Partial failures retain created ids/count.", params: "boardId listId " + cardCreateParams, required: "listId name", write: true, call: s.cardCreate},
		{name: "card_create_batch", description: "Create 1-100 cards with optional checklists; preflight ids, return per-card ids/counts/errors.", params: "boardId listId cards", required: "listId cards", write: true, call: s.cardCreateBatch},
		{name: "card_update", description: "Update card details; move with card_move.", params: "boardId cardId name desc due start dueComplete closed pos idMembers idLabels", required: "cardId", write: true, call: s.cardUpdate},
		{name: "card_archive", description: "Archive a card.", params: "boardId cardId", required: "cardId", write: true, call: s.cardArchive},
		{name: "card_watch", description: "Set card subscription.", params: "boardId cardId subscribed", required: "cardId", write: true, call: s.cardWatch},
		{name: "card_move", description: "Move a card; source and destination boards checked.", params: "boardId cardId listId idBoard pos", required: "cardId listId", write: true, call: s.cardMove},
		{name: "card_copy", description: "Copy a card to an authorized list.", params: "boardId cardId listId idBoard name pos", required: "cardId listId", write: true, call: s.cardCopy},
		{name: "comment_create", description: "Add a card comment.", params: "boardId cardId text", required: "cardId text", write: true, call: s.commentCreate},
		{name: "comment_update", description: "Edit a comment belonging to the card.", params: "boardId cardId commentId text", required: "cardId commentId text", write: true, call: s.commentUpdate},
		{name: "comment_delete", description: "Delete a comment belonging to the card.", params: "boardId cardId commentId", required: "cardId commentId", write: true, call: s.commentDelete},
		{name: "checklist_create", description: "Create a card checklist.", params: "boardId cardId name pos", required: "cardId name", write: true, call: s.checklistCreate},
		{name: "checklist_copy", description: "Copy a checklist to an authorized card.", params: "boardId cardId checklistId name pos", required: "cardId checklistId", write: true, call: s.checklistCopy},
		{name: "checklist_item_create", description: "Add a checklist item.", params: "boardId checklistId name pos checked due memberId", required: "checklistId name", write: true, call: s.checklistItemCreate},
		{name: "checklist_item_find", description: "Find checklist items by description substring.", params: "boardId checklistId nameFilter", required: "checklistId", call: s.checklistItemFind},
		{name: "checklist_item_update", description: "Update an item verified within its checklist.", params: "boardId checklistId itemId name state pos due memberId", required: "checklistId itemId", write: true, call: s.checklistItemUpdate},
		{name: "checklist_item_delete", description: "Delete an item verified within its checklist.", params: "boardId checklistId itemId", required: "checklistId itemId", write: true, call: s.checklistItemDelete},
		{name: "card_member_add", description: "Assign a verified board member to a card.", params: "boardId cardId memberId", required: "cardId memberId", write: true, call: s.cardMemberAdd},
		{name: "card_member_remove", description: "Remove a verified board member from a card.", params: "boardId cardId memberId", required: "cardId memberId", write: true, call: s.cardMemberRemove},
		{name: "label_create", description: "Create a board label.", params: "boardId name color", required: "name", write: true, call: s.labelCreate},
		{name: "label_update", description: "Update an authorized label.", params: "boardId labelId name color", required: "labelId", write: true, call: s.labelUpdate},
		{name: "label_delete", description: "Delete an authorized label.", params: "boardId labelId", required: "labelId", write: true, call: s.labelDelete},
		{name: "customfield_set", description: "Set typed value or dropdown idValue from customfield_find; empty string clears.", params: "boardId cardId customFieldId value idValue", required: "cardId customFieldId", write: true, call: s.customfieldSet},
		{name: "attachment_add", description: "Attach an HTTP(S) URL to a card.", params: "boardId cardId url name", required: "cardId url", write: true, call: s.attachmentAdd},
		{name: "attachment_upload", description: "Upload a local regular file or base64, at most 10 MiB.", params: "boardId cardId path base64 fileName mimeType name", required: "cardId", write: true, call: s.attachmentUpload},
		{name: "attachment_download", description: "Save an attachment privately, at most 10 MiB; no inline body.", params: "boardId cardId attachmentId", required: "cardId attachmentId", write: true, call: s.attachmentDownload},
	}
}
