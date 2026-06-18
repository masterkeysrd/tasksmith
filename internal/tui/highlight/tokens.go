package highlight

var (
	// TasksmithSurface is used for card/container backgrounds.
	TasksmithSurface = Set("TasksmithSurface", Link("NormalNC"))
	// TasksmithContent is used for the main content area (inner background).
	TasksmithContent = Set("TasksmithContent", Link("Normal"))
	// TasksmithContentAlt is used for secondary content areas (e.g. form containers).
	TasksmithContentAlt = Set("TasksmithContentAlt", Link("NormalNC"))
	// TasksmithBorder is used for separators and dividers.
	TasksmithBorder = Set("TasksmithBorder", Link("NormalNC"))
	// TasksmithMuted is used for secondary or dimmed text.
	TasksmithMuted = Set("TasksmithMuted", Link("NormalNC"))
	// TasksmithSubtext is used for readable secondary text.
	TasksmithSubtext = Set("TasksmithSubtext", Link("NormalNC"))
	// TasksmithTag is used for labels and badges.
	TasksmithTag = Set("TasksmithTag", Link("Visual"))

	// TasksmithPrimary is the main accent color (Blue).
	TasksmithPrimary = Set("TasksmithPrimary", Link("Title"))
	// TasksmithSecondary is the secondary accent color (Purple).
	TasksmithSecondary = Set("TasksmithSecondary", Link("NormalNC"))
	// TasksmithAccent is an additional accent color (Yellow/Orange).
	TasksmithAccent = Set("TasksmithAccent", Link("Warn"))

	// Semantic tokens
	TasksmithSuccess = Set("TasksmithSuccess", Link("Hint"))
	TasksmithInfo    = Set("TasksmithInfo", Link("Info"))
	TasksmithWarning = Set("TasksmithWarning", Link("Warn"))
	TasksmithDanger  = Set("TasksmithDanger", Link("Error"))

	// Hover states
	TasksmithPrimaryHover   = Set("TasksmithPrimaryHover", Link("TasksmithPrimary"))
	TasksmithSecondaryHover = Set("TasksmithSecondaryHover", Link("TasksmithSecondary"))
	TasksmithSuccessHover   = Set("TasksmithSuccessHover", Link("TasksmithSuccess"))
	TasksmithDangerHover    = Set("TasksmithDangerHover", Link("TasksmithDanger"))
	TasksmithMutedHover     = Set("TasksmithMutedHover", Link("TasksmithMuted"))
	TasksmithSubtextHover   = Set("TasksmithSubtextHover", Link("TasksmithSubtext"))
)
