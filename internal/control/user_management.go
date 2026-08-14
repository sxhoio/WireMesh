package control

import (
	"fmt"
	"net/http"
	"strings"
)

// findUserInList 在用户列表中定位目标用户。
func findUserInList(users []User, id string) (User, bool) {
	for _, user := range users {
		if user.ID == id {
			return user, true
		}
	}
	return User{}, false
}

// lastActiveAdminCount 统计除指定用户外仍处于启用状态的管理员数量。
func lastActiveAdminCount(users []User, excludeID string) int {
	count := 0
	for _, user := range users {
		if user.ID != excludeID && user.Role == RoleAdmin && user.Active {
			count++
		}
	}
	return count
}

// updateUser 修改用户角色、名称或启用状态（管理员）。
// 守卫：不能修改自己的账号；不能把最后一个启用的管理员降级或停用。
func (a *App) updateUser(w http.ResponseWriter, r *http.Request, c claims) {
	id := r.PathValue("id")
	if id == c.Subject {
		writeError(w, http.StatusBadRequest, "不能修改自己的账号")
		return
	}
	users, err := a.store.ListUsers(c.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户列表失败")
		return
	}
	current, found := findUserInList(users, id)
	if !found {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	var in struct {
		Name   *string `json:"name"`
		Role   *Role   `json:"role"`
		Active *bool   `json:"active"`
	}
	if !decode(w, r, &in) {
		return
	}
	next := current
	if in.Name != nil {
		next.Name = strings.TrimSpace(*in.Name)
		if next.Name == "" {
			writeError(w, http.StatusBadRequest, "名称不能为空")
			return
		}
	}
	if in.Role != nil {
		if *in.Role != RoleViewer && *in.Role != RoleOperator && *in.Role != RoleAdmin {
			writeError(w, http.StatusBadRequest, "角色无效")
			return
		}
		next.Role = *in.Role
	}
	if in.Active != nil {
		next.Active = *in.Active
	}
	// 最后一个启用的管理员不能被降级或停用
	if current.Role == RoleAdmin && current.Active && (next.Role != RoleAdmin || !next.Active) {
		if lastActiveAdminCount(users, current.ID) == 0 {
			writeError(w, http.StatusConflict, "系统必须保留至少一个已启用的管理员")
			return
		}
	}
	if err := a.store.UpdateUser(next); err != nil {
		writeError(w, http.StatusInternalServerError, "更新用户失败")
		return
	}
	if !next.Active {
		a.revokeUserSessions(c.TenantID, next.ID)
	}
	a.auditEvent(c.TenantID, c.Subject, "user.update", "user", next.ID, map[string]string{"role": string(next.Role), "active": fmt.Sprintf("%t", next.Active)})
	writeJSON(w, http.StatusOK, publicUser(next))
}

// deleteUser 删除用户（管理员）。守卫：不能删除自己；不能删除最后一个启用的管理员。
func (a *App) deleteUser(w http.ResponseWriter, r *http.Request, c claims) {
	id := r.PathValue("id")
	if id == c.Subject {
		writeError(w, http.StatusBadRequest, "不能删除自己的账号")
		return
	}
	users, err := a.store.ListUsers(c.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户列表失败")
		return
	}
	current, found := findUserInList(users, id)
	if !found {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if current.Role == RoleAdmin && current.Active && lastActiveAdminCount(users, current.ID) == 0 {
		writeError(w, http.StatusConflict, "系统必须保留至少一个已启用的管理员")
		return
	}
	if err := a.store.DeleteUser(c.TenantID, id); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	// 级联吊销该用户创建的全部 API 令牌，避免删号后遗留管理员权限的长期凭据
	_ = a.store.DeleteAPITokensByCreator(c.TenantID, id)
	a.revokeUserSessions(c.TenantID, id)
	a.auditEvent(c.TenantID, c.Subject, "user.delete", "user", id, map[string]string{"email": current.Email})
	w.WriteHeader(http.StatusNoContent)
}
