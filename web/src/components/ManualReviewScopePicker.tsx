import { useMemo, useState } from "react";
import { useI18n } from "../i18n";
import type { ConsoleData } from "../types";

type ScopeGroup = {
  key: string;
  name: string;
  targetIDs: string[];
};

type ScopeSection = {
  key: "folder" | "tag" | "policy";
  label: string;
  groups: ScopeGroup[];
};

export function ManualReviewScopePicker({
  data,
  selectedTargetIDs,
  onChange,
}: {
  data: ConsoleData;
  selectedTargetIDs: string[];
  onChange: (targetIDs: string[]) => void;
}) {
  const { t } = useI18n();
  const sections = useMemo(() => buildSections(data, t), [data, t]);
  const validTargetIDs = new Set(data.targets.map((target) => target.id));
  const selected = new Set(selectedTargetIDs.filter((id) => validTargetIDs.has(id)));
  const [enabledGroups, setEnabledGroups] = useState<Set<string>>(() => {
    const initial = new Set<string>();
    for (const section of sections) {
      for (const group of section.groups) {
        if (group.targetIDs.every((id) => selected.has(id))) initial.add(group.key);
      }
    }
    return initial;
  });

  const toggleGroup = (section: ScopeSection, group: ScopeGroup, checked: boolean) => {
    const nextGroups = new Set(enabledGroups);
    const nextTargets = new Set(selected);
    if (checked) {
      nextGroups.add(group.key);
      group.targetIDs.forEach((id) => nextTargets.add(id));
    } else {
      nextGroups.delete(group.key);
      const covered = new Set(
        section.groups
          .filter((item) => nextGroups.has(item.key))
          .flatMap((item) => item.targetIDs)
      );
      group.targetIDs.forEach((id) => {
        if (!covered.has(id)) nextTargets.delete(id);
      });
    }
    for (const itemSection of sections) {
      for (const itemGroup of itemSection.groups) {
        if (nextGroups.has(itemGroup.key) && itemGroup.targetIDs.some((id) => !nextTargets.has(id))) {
          nextGroups.delete(itemGroup.key);
        }
      }
    }
    setEnabledGroups(nextGroups);
    onChange(Array.from(nextTargets));
  };

  const toggleTarget = (targetID: string, checked: boolean) => {
    const nextTargets = new Set(selected);
    const nextGroups = new Set(enabledGroups);
    if (checked) {
      nextTargets.add(targetID);
    } else {
      nextTargets.delete(targetID);
      for (const section of sections) {
        for (const group of section.groups) {
          if (group.targetIDs.includes(targetID)) nextGroups.delete(group.key);
        }
      }
    }
    setEnabledGroups(nextGroups);
    onChange(Array.from(nextTargets));
  };

  return (
    <div className="manual-review-scope">
      <div className="manual-review-scope-groups">
        {sections.filter((section) => section.groups.length > 0).map((section) => (
          <section key={section.key} className="manual-review-scope-section">
            <strong>{section.label}</strong>
            <div className="manual-review-scope-options">
              {section.groups.map((group) => {
                const count = group.targetIDs.filter((id) => selected.has(id)).length;
                const enabled = enabledGroups.has(group.key);
                return (
                  <ScopeCheckbox
                    key={group.key}
                    label={group.name}
                    checked={enabled}
                    indeterminate={!enabled && count > 0 && count < group.targetIDs.length}
                    onChange={(checked) => toggleGroup(section, group, checked)}
                  />
                );
              })}
            </div>
          </section>
        ))}
      </div>
      <section className="manual-review-scope-section manual-review-scope-targets">
        <strong>{t("manualReviewScopeTargets")}</strong>
        <div className="manual-review-scope-options">
          {[...data.targets].sort((a, b) => a.name.localeCompare(b.name)).map((target) => (
            <ScopeCheckbox
              key={target.id}
              label={target.name}
              checked={selected.has(target.id)}
              onChange={(checked) => toggleTarget(target.id, checked)}
            />
          ))}
        </div>
      </section>
    </div>
  );
}

function ScopeCheckbox({
  label,
  checked,
  indeterminate = false,
  onChange,
}: {
  label: string;
  checked: boolean;
  indeterminate?: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="manual-review-scope-option">
      <input
        ref={(input) => {
          if (input) input.indeterminate = indeterminate;
        }}
        type="checkbox"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span>{label}</span>
    </label>
  );
}

function buildSections(data: ConsoleData, t: (key: string, fallback?: string) => string): ScopeSection[] {
  const validTargetIDs = new Set(data.targets.map((target) => target.id));
  const folderByID = new Map(data.targetFolders.map((folder) => [folder.id, folder]));
  const folderName = (folderID: string) => {
    const names: string[] = [];
    let current = folderByID.get(folderID);
    while (current) {
      names.unshift(current.name);
      current = current.parent_id ? folderByID.get(current.parent_id) : undefined;
    }
    return names.join(" / ");
  };
  const folders = data.targetFolders
    .map((folder) => ({
      key: `folder:${folder.id}`,
      name: folderName(folder.id),
      targetIDs: data.targets.filter((target) => target.folder_id === folder.id).map((target) => target.id),
    }))
    .filter((group) => group.targetIDs.length > 0);
  const tags = Array.from(new Set(data.targets.flatMap((target) => target.tags || [])))
    .sort((a, b) => a.localeCompare(b))
    .map((tag) => ({
      key: `tag:${tag}`,
      name: tag,
      targetIDs: data.targets.filter((target) => target.tags?.includes(tag)).map((target) => target.id),
    }));
  const policies = data.policies
    .map((policy) => {
      const targetIDs = new Set((policy.target_ids || []).filter((id) => validTargetIDs.has(id)));
      for (const target of data.targets) {
        if ((policy.target_tags || []).some((tag) => target.tags?.includes(tag))) targetIDs.add(target.id);
      }
      return { key: `policy:${policy.id}`, name: policy.name, targetIDs: Array.from(targetIDs) };
    })
    .filter((group) => group.targetIDs.length > 0);
  return [
    { key: "folder", label: t("manualReviewScopeFolders"), groups: folders },
    { key: "tag", label: t("manualReviewScopeTags"), groups: tags },
    { key: "policy", label: t("manualReviewScopePolicies"), groups: policies },
  ];
}
