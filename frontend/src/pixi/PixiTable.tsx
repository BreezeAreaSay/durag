import { useEffect, useRef } from 'react';
import { TableScene } from './TableScene';
import type { SceneModel } from './model';

interface Props {
  model: SceneModel;
  reject: { cardId: string; nonce: number } | null;
  fx: boolean;
  onPlay(cardId: string, targetId?: string): void;
  onTransfer(cardId: string): void;
}

/** Mounts the PixiJS table once and streams model updates into it. */
export function PixiTable({ model, reject, fx, onPlay, onTransfer }: Props) {
  const hostRef = useRef<HTMLDivElement>(null);
  const sceneRef = useRef<TableScene | null>(null);
  const modelRef = useRef(model);
  modelRef.current = model;
  const fxRef = useRef(fx);
  fxRef.current = fx;
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
      { fx: fxRef.current },
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

  return <div ref={hostRef} className="pixi-host" />;
}
