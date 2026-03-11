package cv

func DefaultResume() Resume {
	return Resume{
		Basics: Basics{
			Name:     "Jake Ryan",
			Phone:    "123-456-7890",
			Email:    "jake@su.edu",
			LinkedIn: "https://linkedin.com/in/jake",
			GitHub:   "https://github.com/jake",
		},
		Summary: []string{
			"Seasoned Software Engineer with 8+ years of experience designing scalable backend systems and full-stack applications, specializing in Golang, Python, and TypeScript.",
			"Expert in Domain-Driven Design (DDD) and Clean Architecture, with hands-on experience in building robust backend systems for high-availability environments.",
		},
		Education: []EducationEntry{
			{
				School:   "Southwestern University",
				Location: "Georgetown, TX",
				Degree:   "Bachelor of Arts in Computer Science, Minor in Business",
				Date:     "Aug. 2018 -- May 2021",
			},
			{
				School:   "Blinn College",
				Location: "Bryan, TX",
				Degree:   "Associate's in Liberal Arts",
				Date:     "Aug. 2014 -- May 2018",
			},
		},
		Experience: []ExperienceEntry{
			{
				Company:  "Crimson",
				Role:     "Software Engineer",
				Date:     "Dec 2023 - Present",
				Location: "Bandung, Indonesia",
				Mode:     "Remote",
				Items: []string{
					"Led migration of core services from JavaScript to TypeScript, improving code type safety and reducing runtime errors by 40%.",
					"Architected a Monorepo structure using Node.js Workspaces, consolidating 15+ repositories and reducing dependency conflicts by 70%.",
					"Implemented CI/CD pipelines with GitHub Actions, reducing deployment time from 2 hours to 15 minutes.",
				},
			},
		},
		Projects: []ProjectEntry{
			{
				Name:  "Gomakase",
				Stack: "Golang, Docker, Viper, Cobra",
				Date:  "Jan 2023 - Present",
				Items: []string{
					"Developed a high-performance CLI tool to scaffold production-ready Golang applications.",
					"Engineered an extensible plugin system for dynamic web server generation and database connection management.",
				},
			},
		},
		Skills: []SkillGroup{
			{Name: "Languages", Items: []string{"Golang", "Python", "TypeScript", "SQL", "JavaScript"}},
			{Name: "Frameworks", Items: []string{"Gin", "Django", "NestJS", "Express"}},
			{Name: "Databases", Items: []string{"PostgreSQL", "MongoDB", "Redis"}},
			{Name: "DevOps", Items: []string{"Docker", "Kubernetes", "GitHub Actions", "Nginx"}},
		},
	}
}
