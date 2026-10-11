import { ClipboardList, Inbox, Layers, Plus, Settings, User } from "lucide-react"
import { NavLink, useLocation } from "react-router"

import { useQueue } from "@/api/queue"
import { inView, views, type View } from "@/api/views"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Button } from "@/components/ui/button"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"

const viewIcons: Record<View, typeof Inbox> = { mine: User, unassigned: Inbox, everything: Layers }
const more = [
  { title: "Audit log", path: "/audit", icon: ClipboardList },
  { title: "Settings", path: "/settings", icon: Settings },
]

// The shell of DESIGN-BRIEF §4.0: the factory address on top, New job, the
// three views with counts, More, and who is looking at the foot.
export function AppSidebar() {
  const { queue } = useQueue()
  const { pathname } = useLocation()
  const me = queue?.me ?? ""
  const count = (v: View) => queue?.jobs.filter((j) => inView(v, j, me)).length ?? 0

  return (
    <Sidebar>
      <SidebarHeader>
        <NavLink to="/settings" className="rounded-md px-2 py-1.5 hover:bg-sidebar-accent" data-testid="factory-address">
          <div className="truncate text-sm font-semibold">{queue?.factory.address || "this factory"}</div>
          <div className="truncate text-xs text-muted-foreground">
            codeaf factory{queue?.factory.version ? ` · ${queue.factory.version}` : ""}
          </div>
        </NavLink>
        <Button asChild size="sm" className="justify-start" data-testid="new-job">
          <NavLink to="/new">
            <Plus /> New job
          </NavLink>
        </Button>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Views</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {(Object.keys(views) as View[]).map((v) => {
                const Icon = viewIcons[v]
                return (
                  <SidebarMenuItem key={v}>
                    <SidebarMenuButton asChild isActive={pathname === views[v].path}>
                      <NavLink to={views[v].path} data-testid={`view-${v}`}>
                        <Icon /> {views[v].title}
                      </NavLink>
                    </SidebarMenuButton>
                    {v !== "everything" && count(v) > 0 && <SidebarMenuBadge>{count(v)}</SidebarMenuBadge>}
                  </SidebarMenuItem>
                )
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        <SidebarGroup>
          <SidebarGroupLabel>More</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {more.map((m) => (
                <SidebarMenuItem key={m.path}>
                  <SidebarMenuButton asChild isActive={pathname === m.path}>
                    <NavLink to={m.path}>
                      <m.icon /> {m.title}
                    </NavLink>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter>
        <div className="flex items-center gap-2 px-2 py-1.5">
          <Avatar className="size-7">
            <AvatarFallback>{(me || "?").slice(0, 1).toUpperCase()}</AvatarFallback>
          </Avatar>
          <div className="min-w-0 text-sm">
            <div className="truncate font-medium">{me ? `@${me}` : "not connected"}</div>
          </div>
        </div>
      </SidebarFooter>
    </Sidebar>
  )
}
