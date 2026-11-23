package store

func MALIDOf(animeID int) int {
	if animeID < 0 {
		return -animeID
	}
	return 0
}
