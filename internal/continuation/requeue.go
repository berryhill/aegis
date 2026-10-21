package continuation

// The predecessor mode cannot be confused with retained-draft ordinary Run.
func validSource(i Intent) bool {
	if i.Run.Reexecution == nil {
		return i.RequeueBinding == "" && i.DraftID != "" && i.DraftVersion != 0 && i.PublicationKey != ""
	}
	return i.RequeueBinding != "" && i.DraftID == "" && i.DraftVersion == 0 && i.PublicationKey == "" && !i.Run.Activate && i.Run.QueueItemID == "" && i.Run.Reexecution.PreviousQueue.Validate() == nil && i.Run.Reexecution.PreviousDisposition.Validate() == nil
}
