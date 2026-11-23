package torrent

func pickEpisode(files []File, numbers ...int) (File, int, bool) {
	if len(numbers) == 0 {
		return PickVideo(files)
	}
	return PickInPack(files, numbers[0], numbers, Scope{})
}

func pickNumbered(files []File, episode int, numbers []int) (File, int, bool) {
	return PickInPack(files, episode, numbers, Scope{})
}
