package skills

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// ============================================================
// Skill 管理器 - 从 md 文件加载技能
// ============================================================

type Skill struct {
	Name        string // 技能名称（文件名）
	Description string // 技能描述（md 文件第一行）
	Content     string // 完整内容
	FilePath    string // 文件路径
}

type SkillManager struct {
	skillsDir string                // skills 目录路径
	skills    map[string][]*Skill   // 按智能体名分组
}

// NewSkillManager 创建 Skill 管理器
func NewSkillManager(dataDir string) *SkillManager {
	skillsDir := filepath.Join(dataDir, "skills")
	
	// 确保目录存在
	os.MkdirAll(skillsDir, 0755)
	
	return &SkillManager{
		skillsDir: skillsDir,
		skills:    make(map[string][]*Skill),
	}
}

// LoadAll 加载所有智能体的 skill
func (m *SkillManager) LoadAll() {
	m.skills = make(map[string][]*Skill)
	
	// 遍历 skills 目录下的子目录（每个子目录是一个智能体）
	entries, err := os.ReadDir(m.skillsDir)
	if err != nil {
		log.Printf("读取 skills 目录失败: %v", err)
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		
		agentName := entry.Name()
		agentSkills := m.loadAgentSkills(agentName)
		if len(agentSkills) > 0 {
			m.skills[agentName] = agentSkills
			log.Printf("加载智能体 %s 的 %d 个 skill", agentName, len(agentSkills))
		}
	}
}

// loadAgentSkills 加载指定智能体的所有 skill
func (m *SkillManager) loadAgentSkills(agentName string) []*Skill {
	agentDir := filepath.Join(m.skillsDir, agentName)
	
	entries, err := os.ReadDir(agentDir)
	if err != nil {
		return nil
	}

	skills := make([]*Skill, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		filePath := filepath.Join(agentDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			log.Printf("读取 skill 文件失败: %s, %v", filePath, err)
			continue
		}

		content := string(data)
		name := strings.TrimSuffix(entry.Name(), ".md")
		
		// 提取描述（第一行去掉 # 号）
		description := ""
		lines := strings.SplitN(content, "\n", 2)
		if len(lines) > 0 {
			description = strings.TrimPrefix(lines[0], "#")
			description = strings.TrimSpace(description)
		}

		skills = append(skills, &Skill{
			Name:        name,
			Description: description,
			Content:     content,
			FilePath:    filePath,
		})
	}

	return skills
}

// GetAgentSkills 获取指定智能体的 skill 列表
func (m *SkillManager) GetAgentSkills(agentName string) []*Skill {
	return m.skills[agentName]
}

// GetAgentSkillsPrompt 获取智能体的 skill 提示词（用于拼接到系统提示词）
func (m *SkillManager) GetAgentSkillsPrompt(agentName string) string {
	skills := m.skills[agentName]
	if len(skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n## 你拥有的技能\n")
	sb.WriteString("你可以使用以下技能来帮助用户：\n\n")

	for i, skill := range skills {
		sb.WriteString(fmt.Sprintf("### 技能 %d: %s\n", i+1, skill.Name))
		sb.WriteString(skill.Description + "\n\n")
	}

	sb.WriteString("当用户的问题涉及以上技能时，请主动使用相关技能来回答。\n")

	return sb.String()
}

// Reload 重新加载所有 skill
func (m *SkillManager) Reload() {
	m.LoadAll()
}

// CreateAgentSkillDir 为智能体创建 skill 目录
func (m *SkillManager) CreateAgentSkillDir(agentName string) error {
	agentDir := filepath.Join(m.skillsDir, agentName)
	return os.MkdirAll(agentDir, 0755)
}

// AddSkill 添加一个 skill 文件
func (m *SkillManager) AddSkill(agentName, skillName, content string) error {
	// 确保目录存在
	if err := m.CreateAgentSkillDir(agentName); err != nil {
		return err
	}

	filePath := filepath.Join(m.skillsDir, agentName, skillName+".md")
	return os.WriteFile(filePath, []byte(content), 0644)
}

// DeleteSkill 删除一个 skill 文件
func (m *SkillManager) DeleteSkill(agentName, skillName string) error {
	filePath := filepath.Join(m.skillsDir, agentName, skillName+".md")
	return os.Remove(filePath)
}

// ListAgents 列出有 skill 的智能体
func (m *SkillManager) ListAgents() []string {
	agents := make([]string, 0, len(m.skills))
	for agentName := range m.skills {
		agents = append(agents, agentName)
	}
	return agents
}
