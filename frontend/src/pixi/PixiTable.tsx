import { useEffect, useRef } from 'react';
import { TableScene, type SceneLabels } from './TableScene';
import type { SceneModel } from './model';

interface Props {
  model: SceneModel;
  reject: { cardId: string; nonce: number } | null;
  fx: boolean;
  /** Height of the HTML panel at the top; the table keeps clear of it. */
  topInset: number;
  labels: SceneLabels;
  onPlay(cardId: string, targetId?: string): void;
  onTransfer(cardId: string): void;
}

/** Mounts the PixiJS table once and streams model updates into it. */
export function PixiTable({ model, reject, fx, topInset, labels, onPlay, onTransfer }: Props) {
  const hostRef = useRef<HTMLDivElement>(null);
  const sceneRef = useRef<TableScene | null>(null);
  const modelRef = useRef(model);
  modelRef.current = model;
  const fxRef = useRef(fx);
  fxRef.current = fx;
  const topRef = useRef(topInset);
  topRef.current = topInset;
  const labelsRef = useRef(labels);
  labelsRef.current = labels;
  const handlersRef = useRef({ onPlay, onTransfer });
  handlersRef.current = { onPlay, onTransfer };

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    let cancelled = false;
    const scene = new TableScene(
      {
        onPlay: (id, target) => handlersRef.current.onPlay(id, target),
        onTransfer: (id) => handlersRef.current.onTransfer(id),
      },
      { fx: fxRef.current, topInset: topRef.current, labels: labelsRef.current },
    );
    scene
      .init(host)
      .then(() => {
        if (cancelled) {
          scene.destroy();
          return;
        }
        sceneRef.current = scene;
        scene.setFx(fxRef.current);
        scene.update(modelRef.current);
      })
      .catch((err: unknown) => {
        console.error('PixiJS failed to start', err);
      });
    return () => {
      cancelled = true;
      sceneRef.current = null;
      scene.destroy();
    };
  }, []);

  useEffect(() => {
    sceneRef.current?.update(model);
  }, [model]);

  useEffect(() => {
    if (reject) sceneRef.current?.rejectCard(reject.cardId);
  }, [reject]);

  useEffect(() => {
    sceneRef.current?.setFx(fx);
  }, [fx]);

  useEffect(() => {
    sceneRef.current?.setTopInset(topInset);
  }, [topInset]);

  return <div ref={hostRef} className="pixi-host" />;
}
