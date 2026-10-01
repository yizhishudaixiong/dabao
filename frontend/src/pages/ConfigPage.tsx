import { Card, PathField, TextInput } from '../components/ui'
import type { BuildConfig } from '../types/models'

export default function ConfigPage({
  cfg,
  updateCfg,
}: {
  cfg: BuildConfig
  updateCfg: (patch: Partial<BuildConfig>) => void
}) {
  const pickEntry = async () => {
    const p = await window.go.main.App.SelectEntryScript()
    if (p) updateCfg({ entryScript: p })
  }
  const pickIcon = async () => {
    const p = await window.go.main.App.SelectIcon()
    if (p) updateCfg({ iconPath: p })
  }
  const pickOutput = async () => {
    const p = await window.go.main.App.SelectOutputDir()
    if (p) updateCfg({ outputDir: p })
  }

  return (
    <div className="drop-zone space-y-5">
      <Card
        title="程序信息"
        desc="这些信息将写入最终 EXE 的文件属性中"
      >
        <div className="grid grid-cols-3 gap-4">
          <TextInput
            label="程序名称 *"
            placeholder="如 MyApp"
            value={cfg.name}
            onChange={(e) => updateCfg({ name: e.target.value })}
          />
          <TextInput
            label="版本 *"
            placeholder="如 1.0.0"
            value={cfg.version}
            onChange={(e) => updateCfg({ version: e.target.value })}
          />
          <TextInput
            label="作者"
            placeholder="作者 / 公司名"
            value={cfg.author}
            onChange={(e) => updateCfg({ author: e.target.value })}
          />
        </div>

        <div className="mt-4 grid grid-cols-2 gap-4">
          <div>
            <PathField
              label="入口脚本（.py）"
              placeholder="选择要打包的 Python 程序"
              value={cfg.entryScript}
              onPick={pickEntry}
            />
            <p className="mt-1.5 text-xs leading-relaxed text-ink-faint">
              入口脚本 = 程序运行时最先执行的文件（通常是 main.py 或 app.py）。
              请选择你程序真正的「主入口」文件：工具会分析它以及它引用的全部代码，把它们一起打包成 EXE。
            </p>
          </div>
          <div>
            <PathField
              label="程序图标（.ico / .png / .jpg）"
              placeholder="选择 .ico / .png / .jpg，非 ico 将自动转换"
              value={cfg.iconPath}
              onPick={pickIcon}
            />
          </div>
        </div>
        <p className="mt-3 text-xs leading-relaxed text-ink-faint">
          💡 支持拖拽：把 .py 文件拖到本页自动设为入口脚本，把图片（png/jpg/ico）拖到本页自动设为程序图标。
        </p>
      </Card>

      <Card title="输出位置" desc="打包完成后的最终产物将保存到此处">
        <PathField
          placeholder="选择最终 EXE 的输出目录"
          value={cfg.outputDir}
          onPick={pickOutput}
        />
      </Card>
    </div>
  )
}
