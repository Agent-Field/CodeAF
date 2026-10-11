import { useCallback, useEffect, useRef, useState } from "react"

import { api } from "./client"
import { sentence } from "./job"
import type { GitHubLink, Me, RepoList } from "./types"

export interface SettingsRead {
  github: GitHubLink | null
  repos: RepoList | null
  me: Me | null
  error: string | null
  connect: (token: string) => Promise<void>
  watch: (full: string, on: boolean) => Promise<void>
}

// Three independent reads. One failure is a sentence; the cards that did
// load still draw, so a missing dry-run endpoint cannot blank the page.
export function useSettings(): SettingsRead {
  const [github, setGithub] = useState<GitHubLink | null>(null)
  const [repos, setRepos] = useState<RepoList | null>(null)
  const [me, setMe] = useState<Me | null>(null)
  const [error, setError] = useState<string | null>(null)

  // The ref is the list the next PUT builds on. It is updated only here, so a
  // render cannot put back a list a switch has already moved past.
  const reposRef = useRef<RepoList | null>(null)
  const keepRepos = useCallback((list: RepoList) => {
    const normalized = { ...list, watched: list.watched ?? [], available: list.available ?? [] }
    reposRef.current = normalized
    setRepos(normalized)
  }, [])

  const load = useCallback(() => {
    api.github().then(setGithub, (e: unknown) => setError(sentence(e)))
    api.repos().then(keepRepos, (e: unknown) => setError(sentence(e)))
    api.me().then(setMe, (e: unknown) => setError(sentence(e)))
  }, [keepRepos])

  useEffect(load, [load])

  const connect = useCallback(async (token: string) => {
    try {
      await api.connectGitHub(token)
      setGithub(await api.github())
      setError(null)
    } catch (e) {
      setError(sentence(e))
    }
  }, [])

  // Writes run one at a time. PUT replaces the whole watched list, so a second
  // switch has to see the first switch's list or it would put the old one back.
  const tail = useRef(Promise.resolve())
  const watch = useCallback((full: string, on: boolean) => {
    const job = tail.current.then(async () => {
      const have = reposRef.current?.watched ?? []
      const next = on ? [...have.filter((name) => name !== full), full] : have.filter((name) => name !== full)
      if (reposRef.current) keepRepos({ ...reposRef.current, watched: next })
      try {
        await api.setRepos(next)
        keepRepos(await api.repos())
        setError(null)
      } catch (e) {
        setError(sentence(e))
        await api.repos().then(keepRepos, () => {})
      }
    })
    tail.current = job.then(() => {}, () => {})
    return job
  }, [keepRepos])

  return { github, repos, me, error, connect, watch }
}
