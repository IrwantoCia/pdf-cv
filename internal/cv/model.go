package cv

type Resume struct {
	Basics     Basics            `json:"basics"`
	Summary    []string          `json:"summary"`
	Education  []EducationEntry  `json:"education"`
	Experience []ExperienceEntry `json:"experience"`
	Projects   []ProjectEntry    `json:"projects"`
	Skills     []SkillGroup      `json:"skills"`
}

type Basics struct {
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
	LinkedIn string `json:"linkedin"`
	GitHub   string `json:"github"`
}

type EducationEntry struct {
	School   string `json:"school"`
	Location string `json:"location"`
	Degree   string `json:"degree"`
	Date     string `json:"date"`
}

type ExperienceEntry struct {
	Company  string   `json:"company"`
	Role     string   `json:"role"`
	Date     string   `json:"date"`
	Location string   `json:"location"`
	Mode     string   `json:"mode"`
	Items    []string `json:"items"`
}

type ProjectEntry struct {
	Name  string   `json:"name"`
	Stack string   `json:"stack"`
	Date  string   `json:"date"`
	Items []string `json:"items"`
}

type SkillGroup struct {
	Name  string   `json:"name"`
	Items []string `json:"items"`
}
