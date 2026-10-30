// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router";
import {
  checkModelConnection,
  errorMessage,
  getSystemStatus,
  type ModelCheck,
  type SystemStatus
} from "../../api";
import { useI18n } from "../../i18n";
import Icon from "./Icon";
import Modal from "./Modal";

interface InferenceTrace {
  mode: "fixture" | "real";
  at: string;
}

const storageKey = "dw:last-ai-inference";

function readInference(): InferenceTrace | null {
  try {
    const raw = window.sessionStorage.getItem(storageKey);
    if (!raw) {
      return null;
    }
    const parsed = JSON.parse(raw) as InferenceTrace;
    return parsed.mode === "fixture" || parsed.mode === "real" ? parsed : null;
  } catch {
    return null;
  }
}

function checkLabel(status: ModelCheck["status"], zh: boolean): string {
  const labels: Record<ModelCheck["status"], [string, string]> = {
    fixture: ["Fixture 模式 · 未联系服务商", "Fixture mode · no provider contacted"],
    unverified_model: ["服务商端点可达 · 模型可调用性尚未验证", "Provider endpoint reachable · model callability not verified"],
    unauthorized: ["服务商拒绝了服务器凭证", "Provider rejected the server credentials"],
    rate_limited: ["服务商正在限制检测请求", "Provider is rate-limiting checks"],
    unavailable: ["服务商暂时不可用", "Provider is temporarily unavailable"],
    tls_failure: ["TLS 验证失败", "TLS verification failed"],
    invalid_url: ["服务器配置的模型地址无效", "The server model URL is invalid"],
    unreachable: ["无法连接服务商端点", "Provider endpoint unreachable"],
    error: ["服务商返回异常响应", "Unexpected provider response"]
  };
  const label = labels[status];
  return zh ? label[0] : label[1];
}

export default function AISettingsDialog({
  open,
  onClose
}: {
  open: boolean;
  onClose: () => void;
}) {
  const { lang } = useI18n();
  const zh = lang === "zh";
  const [system, setSystem] = useState<SystemStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [check, setCheck] = useState<ModelCheck | null>(null);
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState("");
  const [inference, setInference] = useState<InferenceTrace | null>(null);

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    setError("");
    try {
      const status = await getSystemStatus(signal);
      if (!signal?.aborted) {
        setSystem(status);
      }
    } catch (failure) {
      if (!signal?.aborted) {
        setError(errorMessage(failure));
      }
    } finally {
      if (!signal?.aborted) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    if (!open) {
      return;
    }
    const controller = new AbortController();
    setCheck(null);
    setInference(readInference());
    void load(controller.signal);
    const onInference = (event: Event) => {
      const detail = (event as CustomEvent<InferenceTrace>).detail;
      if (detail) {
        setInference(detail);
      }
    };
    window.addEventListener("dw:ai-inference", onInference);
    return () => {
      controller.abort();
      window.removeEventListener("dw:ai-inference", onInference);
    };
  }, [open, load]);

  async function runCheck() {
    setChecking(true);
    setError("");
    try {
      setCheck(await checkModelConnection());
    } catch (failure) {
      setError(errorMessage(failure));
    } finally {
      setChecking(false);
    }
  }

  const mode = system?.model_mode ?? null;
  const real = mode === "real";

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={zh ? "连接你的 Personal AI" : "Connect your Personal AI"}
      kicker={zh ? "AI SETTINGS / SERVER-MANAGED" : "AI SETTINGS / SERVER-MANAGED"}
      wide
      actions={
        <>
          <Link className="dw-button dw-button-ghost" to="/settings#ai-connection" onClick={onClose}>
            {zh ? "部署与账户设置" : "Deployment & account settings"}
          </Link>
          <button
            className="dw-button dw-button-primary"
            type="button"
            disabled={checking || !system}
            onClick={() => void runCheck()}
          >
            <Icon name="shield" size={16} />
            {checking ? (zh ? "正在检测…" : "Checking…") : zh ? "检测服务商连通性" : "Check provider reachability"}
          </button>
        </>
      }
    >
      <p className="dw-modal-lead">
        {zh
          ? "实例管理员从服务器侧连接 AI。浏览器只显示运行状态与连通性，不读取、不回显、也不保存部署级 API Key。"
          : "The instance administrator connects AI on the server. This browser only displays status and reachability; it never reads, echoes, or stores deployment API keys."}
      </p>

      <div className={`dw-ai-status${real ? " is-real" : ""}`}>
        <span className="dw-ai-status-icon">
          <Icon name={real ? "spark" : "shield"} size={20} />
        </span>
        <div>
          <strong>
            {loading
              ? zh
                ? "正在读取部署状态…"
                : "Reading deployment status…"
              : real
                ? zh
                  ? "Real · 服务器已配置真实模型"
                  : "Real · a real model is configured on the server"
                : zh
                  ? "Fixture · 当前只有确定性演示输出"
                  : "Fixture · only deterministic demo output is active"}
          </strong>
          <small>
            {real
              ? zh
                ? "真实推理仍只会在你主动与 Personal AI 对话时发生。"
                : "Real inference happens only when you actively talk to your Personal AI."
              : zh
                ? "Fixture 输出不是真实模型推理，也不会联系模型服务商。"
                : "Fixture output is not real model inference and never contacts a model provider."}
          </small>
        </div>
      </div>

      <div className="dw-ai-facts">
        <div>
          <span>{zh ? "模型模式" : "Model mode"}</span>
          <strong className={real ? "is-real" : ""}>{mode ? mode.toUpperCase() : "—"}</strong>
        </div>
        <div>
          <span>{zh ? "模型名称（非密钥）" : "Model name (not a secret)"}</span>
          <strong>
            {system?.model_name ||
              (real
                ? zh
                  ? "未配置模型名称"
                  : "No model name configured"
                : zh
                  ? "Fixture 演示（未连接服务商）"
                  : "Fixture demo (no provider)")}
          </strong>
        </div>
        <div>
          <span>{zh ? "API Key 所在位置" : "API key location"}</span>
          <strong>{zh ? "仅服务器环境" : "Server environment only"}</strong>
        </div>
        <div>
          <span>{zh ? "服务商兼容性" : "Provider compatibility"}</span>
          <strong>{zh ? "任意 OpenAI 兼容服务" : "Any OpenAI-compatible service"}</strong>
        </div>
      </div>

      <div className="dw-ai-check">
        <div>
          <h3>{zh ? "连通性检测不是推理" : "A reachability check is not inference"}</h3>
          <p>
            {zh
              ? "“检测服务商连通性”只访问服务商的模型列表端点，不发送提示词，不产生补全调用。它最多证明端点可达，不能证明模型名称可调用，也不能证明已经完成真实推理。"
              : "The check contacts the provider's model-list endpoint only. It sends no prompt and makes no completion call. It can prove endpoint reachability, but cannot prove the configured model is callable or that real inference has happened."}
          </p>
        </div>
        {check ? (
          <div className={`dw-ai-check-result is-${check.status}`} role="status">
            <strong>{checkLabel(check.status, zh)}</strong>
            <small>{zh ? "服务端返回：" : "Server response: "}{check.detail}</small>
          </div>
        ) : null}
      </div>

      <div className="dw-ai-inference">
        <span>{zh ? "本次浏览器会话最近一次 AI 回复" : "Latest AI reply in this browser session"}</span>
        <strong>
          {inference
            ? inference.mode === "real"
              ? zh
                ? "Real · 已完成一次真实模型推理"
                : "Real · one real model inference completed"
              : zh
                ? "Fixture · 确定性演示，不是真实推理"
                : "Fixture · deterministic demo, not real inference"
            : zh
              ? "尚无 AI 回复"
              : "No AI reply yet"}
        </strong>
      </div>

      <div className="dw-ai-safe-config">
        <h3>
          <Icon name="lock" size={16} />
          {zh ? "真实部署如何安全配置？" : "How real deployments configure AI safely"}
        </h3>
        <ol>
          <li>
            {zh
              ? "在 Windows 自托管实例的本机启动器中选择「菜单 6 · 打开 AI 配置文件」。"
              : "On the Windows self-hosted instance, open the local launcher and choose “Menu 6 · Open AI configuration”."}
          </li>
          <li>
            {zh
              ? "在服务器文件中填写服务商地址、模型名称和密钥；密钥不会进入浏览器。"
              : "Fill in the provider URL, model name, and key in the server file; the key never enters the browser."}
          </li>
          <li>
            {zh
              ? "使用「菜单 7 · 应用并验证」重建实例。随后回到这里查看 Real 状态和连通性。"
              : "Use “Menu 7 · Apply and verify” to rebuild the instance. Return here to see Real status and reachability."}
          </li>
        </ol>
        <p>
          {zh
            ? "不要把生产 API Key 粘贴到网页或任何原型中。Derive World 不提供浏览器侧密钥输入，也不会用“保存成功”的提示假装模型已配置。"
            : "Never paste a production API key into a web page or prototype. Derive World has no browser-side key input and never claims a model is configured through a “saved” message alone."}
        </p>
      </div>

      {error ? <p className="dw-form-error" role="alert">{error}</p> : null}
    </Modal>
  );
}
