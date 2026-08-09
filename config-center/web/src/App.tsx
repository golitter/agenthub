import { useEffect, useMemo, useState } from "react"
import { applyProfile, bootstrap, getConfig, getProfiles, restoreBackup, saveConfig, validateConfig } from "./api"
import { BackupManager } from "./components/BackupManager"
import { TemplateEditor } from "./components/TemplateEditor"
import { changedFileCount } from "./draft"
import type { ApplyResult, BackupEntry, ConfigResponse, Draft, Profile, ProfileId, SaveResult } from "./schema"

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "操作失败"
}

function scrollToSection(id: string) {
  document.getElementById(id)?.scrollIntoView({ behavior: "smooth", block: "start" })
}

type BusyAction = "profile" | "save" | "apply" | "restore"

export default function App() {
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [profilesLoading, setProfilesLoading] = useState(true)
  const [profile, setProfile] = useState<ProfileId | null>(null)
  const [config, setConfig] = useState<ConfigResponse | null>(null)
  const [draft, setDraft] = useState<Draft>({})
  const [busyAction, setBusyAction] = useState<BusyAction | null>(null)
  const [error, setError] = useState("")
  const [result, setResult] = useState<SaveResult | null>(null)
  const [applyResult, setApplyResult] = useState<ApplyResult | null>(null)
  const changed = useMemo(() => changedFileCount(draft), [draft])
  const busy = busyAction !== null

  useEffect(() => {
    void (async () => {
      try {
        await bootstrap()
        setProfiles(await getProfiles())
      } catch (cause) {
        setError(`无法连接配置中心 API：${errorMessage(cause)}`)
      } finally {
        setProfilesLoading(false)
      }
    })()
  }, [])

  async function chooseProfile(next: ProfileId) {
    if (changed > 0 && !window.confirm("切换环境会丢弃当前未保存修改，是否继续？")) return
    setBusyAction("profile")
    setError("")
    setResult(null)
    setApplyResult(null)
    try {
      setConfig(await getConfig(next))
      setProfile(next)
      setDraft({})
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusyAction(null)
    }
  }

  function returnToProfiles() {
    if (changed > 0 && !window.confirm("切换环境会丢弃当前未保存修改，是否继续？")) return
    setProfile(null)
    setConfig(null)
    setDraft({})
    setResult(null)
    setApplyResult(null)
    requestAnimationFrame(() => scrollToSection("profile-selection"))
  }

  async function save() {
    if (!profile || !config || changed === 0) return
    setBusyAction("save")
    setError("")
    try {
      const validation = await validateConfig(profile, draft)
      if (!validation.ok) {
        setError(validation.issues.map((issue) => `${issue.fileId}: ${issue.message}`).join("；"))
        return
      }
      const saved = await saveConfig(profile, draft)
      setConfig(await getConfig(profile))
      setDraft({})
      setResult(saved)
      setApplyResult(null)
      requestAnimationFrame(() => scrollToSection("apply-run"))
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusyAction(null)
    }
  }

  async function apply() {
    if (!profile || changed > 0) return
    const label = profile === "local" ? "重启本地三端" : "构建并启动 Docker 服务，然后重启 AgentEnd"
    if (!window.confirm(`${label}？运行中的服务会短暂中断。`)) return
    setBusyAction("apply")
    setError("")
    setApplyResult(null)
    try {
      const applied = await applyProfile(profile)
      setApplyResult(applied)
      if (!applied.ok) setError(`应用运行失败，退出码 ${applied.exitCode}。请查看下方输出。`)
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusyAction(null)
    }
  }

  async function restore(backup: BackupEntry) {
    if (!profile) return
    setBusyAction("restore")
    setError("")
    try {
      await restoreBackup(profile, backup.fileId, backup.id, backup.currentRevision)
      setConfig(await getConfig(profile))
      setDraft({})
      setResult(null)
      setApplyResult(null)
    } catch (cause) {
      setError(errorMessage(cause))
      throw cause
    } finally {
      setBusyAction(null)
    }
  }

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="product-identity">
          <span className="product-mark" aria-hidden="true"><img src="/favicon.svg" alt="" /></span>
          <div>
            <p className="eyebrow">AgentHub · Operations</p>
            <h1>配置文件工作台</h1>
            <p className="intro">Example / actual 双栏编辑 · dotenv · YAML · JSON</p>
          </div>
        </div>
        <div className="header-actions">
          {config && <div className="header-stat"><span>FILE PAIRS</span><strong>{config.files.length}</strong></div>}
          {profile && <button className="profile-current" type="button" disabled={busy} onClick={returnToProfiles}><span>PROFILE</span><strong>{profile.toUpperCase()}</strong><small>切换环境</small></button>}
        </div>
      </header>

      <main id="main-content" className="main-content">
        <nav className="compact-steps" aria-label="配置步骤">
          <button type="button" className={profile ? "is-done" : "is-active"} onClick={profile ? returnToProfiles : () => scrollToSection("profile-selection")}><b>1</b> 选择环境</button>
          <button type="button" disabled={!profile} className={profile && changed === 0 && !result ? "is-active" : profile ? "is-done" : ""} onClick={() => scrollToSection("file-editor")}><b>2</b> 对照填写</button>
          <button type="button" disabled={!profile} className={changed > 0 ? "is-active" : result ? "is-done" : ""} onClick={() => scrollToSection("save-review")}><b>3</b> 审查保存</button>
          <button type="button" disabled={!profile} className={applyResult?.ok ? "is-done" : result && changed === 0 ? "is-active" : ""} onClick={() => scrollToSection("apply-run")}><b>4</b> 应用运行</button>
        </nav>

        {error && <div className="error-banner" role="alert"><strong>无法继续</strong><p>{error}</p></div>}

        {!profile ? (
          <section id="profile-selection" className="profile-picker">
            <header><p className="eyebrow">Step 1</p><h2>选择要填写的配置环境</h2></header>
            {profilesLoading ? (
              <div className="profile-loading" aria-label="正在加载配置环境" aria-busy="true"><span /><span /></div>
            ) : <div className="profile-options">
              {profiles.map((item) => (
                <button key={item.id} type="button" disabled={!item.available || busy} onClick={() => void chooseProfile(item.id)}>
                  <span className="profile-code">{item.id === "local" ? "LCL" : "DKR"}</span>
                  <strong>{item.title}</strong>
                  <p>{item.description}</p>
                  <small>{item.available ? `${item.fileCount} 组 example/actual 文件` : `不可用 · 缺少 ${item.missing.length} 个模板文件`}</small>
                </button>
              ))}
            </div>}
          </section>
        ) : config ? (
          <>
            <section id="file-editor" className="workspace-heading">
              <div><p className="eyebrow">Step 2</p><h2>逐行对照两个完整文件</h2><p>左侧 example 不可修改，右侧 actual 直接编辑；密码等实际值也在右侧文件中查看。</p></div>
              <div className="workspace-count"><strong>{changed}</strong><span>个文件待保存</span></div>
            </section>

            <TemplateEditor files={config.files} draft={draft} disabled={busy} onDraft={(next) => { setDraft(next); setResult(null); setApplyResult(null) }} />

            <section id="save-review" className="save-panel">
              <div>
                <p className="eyebrow">Step 3</p>
                <h2>{changed > 0 ? `校验并保存 ${changed} 个文件` : "修改会在这里汇总"}</h2>
                <p>{changed > 0 ? "保存前会检查 revision、创建备份并原子替换实际文件。" : "当前没有磁盘写入操作。"}</p>
              </div>
              <div className="save-actions">
                {changed > 0 && <button className="text-button" type="button" disabled={busy} onClick={() => setDraft({})}>放弃修改</button>}
                <button className="primary-button" type="button" disabled={busy || changed === 0} onClick={() => void save()}>{busyAction === "save" ? "保存中…" : `保存 ${changed || ""} 个文件`}</button>
              </div>
            </section>

            {result && <div className="success-banner" role="status"><strong>实际配置已保存</strong><p>已写入 {result.saved.length} 个 actual 文件。配置如何生效由主项目的运行或部署流程决定。</p></div>}

            <section id="apply-run" className="run-panel">
              <div>
                <p className="eyebrow">Step 4</p>
                <h2>应用配置并运行</h2>
                <p>{profile === "local" ? "执行 make restart，重启本地 Frontend、Backend 与 AgentEnd。" : "执行 make docker-up 构建并启动容器，再重启宿主机 AgentEnd。"}</p>
                {changed > 0 && <small>请先保存右侧 actual 文件，再运行服务。</small>}
              </div>
              <button className="run-button" type="button" disabled={busy || changed > 0} onClick={() => void apply()}>{busyAction === "apply" ? "运行中…" : profile === "local" ? "重启本地服务" : "构建并运行 Docker"}</button>
            </section>

            {applyResult && <section className={`run-output ${applyResult.ok ? "is-success" : "is-failed"}`} aria-live="polite"><header><strong>{applyResult.ok ? "运行完成" : `运行失败 · exit ${applyResult.exitCode}`}</strong><span>{applyResult.commands.map((command) => command.join(" ")).join(" → ")}</span></header><pre>{applyResult.output || "命令执行完成，没有额外输出。"}</pre></section>}

            <details className="recovery-section">
              <summary>备份与恢复</summary>
              <BackupManager profile={profile} refreshKey={config.files.map((file) => file.revision).join(":")} disabled={busy} hasDraft={changed > 0} onRestore={restore} />
            </details>
          </>
        ) : null}
      </main>
    </div>
  )
}
