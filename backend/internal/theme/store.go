package theme

type StoreView struct {
	Title, URL, Back, BackURL                                               string
	Search, SearchLabel, Submit, CategoryLabel, Empty, Next, NextURL, Error string
	Categories                                                              []Choice
	Products                                                                []StoreProduct
	Product                                                                 *StoreProduct
}

type StoreProduct struct {
	Title, Description, URL, Price string
	Images                         []StoreImage
	Variants                       []StoreVariant
}

type StoreImage struct{ URL, Alt string }

type StoreVariant struct{ SKU, Name, Price, Availability string }
