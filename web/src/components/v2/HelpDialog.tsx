// SPDX-License-Identifier: Apache-2.0
import { useI18n } from "../../i18n";
import Icon from "./Icon";
import Modal from "./Modal";

export default function HelpDialog({
  open,
  onClose
}: {
  open: boolean;
  onClose: () => void;
}) {
  const { lang } = useI18n();
  const zh = lang === "zh";
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={zh ? "直接去创造。" : "Go straight to creating."}
      kicker={zh ? "QUICK JOURNEY" : "QUICK JOURNEY"}
      actions={
        <button className="dw-button dw-button-primary" type="button" onClick={onClose}>
          {zh ? "好，开始探索" : "Start exploring"}
          <Icon name="arrow" size={16} />
        </button>
      }
    >
      <p className="dw-modal-lead">
        {zh ? (
          <>
            这里只需要掌握一个原则：
            <strong>不要先找功能，直接触碰你想改变的世界。</strong>
          </>
        ) : (
          <>
            One principle is enough: <strong>don’t look for a feature first — touch the world you want to change.</strong>
          </>
        )}
      </p>
      <div className="dw-choice-list">
        <div className="dw-choice">
          <Icon name="world" size={20} />
          <div>
            <strong>{zh ? "我的世界" : "My world"}</strong>
            <small>
              {zh
                ? "点击球形对象，就地添加、编辑或完成内容；拖动节点安排视角；从节点旁的衍生手柄拉出新分支。"
                : "Tap a spherical object to add, edit, or complete content in place. Drag nodes to arrange your view. Pull the branch handle to derive."}
            </small>
          </div>
        </div>
        <div className="dw-choice">
          <Icon name="together" size={20} />
          <div>
            <strong>{zh ? "一起创造" : "Create together"}</strong>
            <small>
              {zh
                ? "与好友私信、查找并添加好友，在真实的共同世界里贡献想法。好友仍需单独的世界邀请。"
                : "Message friends, find and add people, and contribute to real shared worlds. Friendship still requires a separate world invitation."}
            </small>
          </div>
        </div>
        <div className="dw-choice">
          <Icon name="explore" size={20} />
          <div>
            <strong>{zh ? "探索世界" : "Explore worlds"}</strong>
            <small>
              {zh
                ? "浏览作者主动公开的版本；如果允许衍生，你可以创建属于自己的、默认私人的独立分支。"
                : "Browse versions people chose to publish. When derivation is allowed, create your own independent branch, private by default."}
            </small>
          </div>
        </div>
      </div>
      <p className="dw-fineprint">
        <Icon name="help" size={13} />
        {zh
          ? "快捷键：在首页按 N 创建世界 · ? 打开指南 · Esc 关闭面板 · 滚轮缩放舞台。所有节点都能用键盘选择。"
          : "Shortcuts: on Home, N creates a world · ? opens this guide · Esc closes panels · wheel zooms the stage. Every node is keyboard selectable."}
      </p>
    </Modal>
  );
}
