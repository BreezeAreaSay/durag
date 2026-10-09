import { useEffect, useRef, useState } from 'react';
import { TableScene, type SceneLabels } from './TableScene';
import type { SceneModel } from './model';
import type { Connection } from '../state/GameContext';

interface Props {
  model: SceneModel;
  reject: { cardId: string; nonce: number } | null;
  /** Height of the HTML panel at the top; the table keeps clear of it. */
  topInset: number;
  labels: SceneLabels;
  connection: Connection;
  onPlay(cardId: string, targetId?: string): void;
  onTransfer(cardId: string): void;
  onTakeStump(): void;
}

declare global {
  interface Window {
    /** read-only scene diagnostics, present only with `?debug=1` */
    __durag?: TableScene;
  }
}

const DEBUG = typeof location !== 'undefined' && new URLSearchParams(location.search).has('debug');

/** Mounts the PixiJS table once and streams model updates into it. */
export function PixiTable({ model, reject, topInset, labels, connection, onPlay, onTransfer, onTakeStump }: Props) {
  const hostRef = useRef<HTMLDivElement>(null);
  const sceneRef = useRef<TableScene | null>(null);
  // bumped when the WebGL context is lost: the scene is torn down and rebuilt
  const [generation, setGeneration] = useState(0);
  const modelRef = useRef(model);
  modelRef.current = model;
  const topRef = useRef(topInset);
  topRef.current = topInset;
  const labelsRef = useRef(labels);
  labelsRef.current = labels;
  const handlersRef = useRef({ onPlay, onTransfer, onTakeStump });
  handlersRef.current = { onPlay, onTransfer, onTakeStump };

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    let cancelled = false;
    const scene = new TableScene(
      {
        onPlay: (id, target) => handlersRef.current.onPlay(id, target),
        onTransfer: (id) => handlersRef.current.onTransfer(id),
        onTakeStump: () => handlersRef.current.onTakeStump(),
        onContextLost: () => {
          console.warn('WebGL context lost: rebuilding the table');
          setGeneration((g) => g + 1);
        },
      },
      { topInset: topRef.current, labels: labelsRef.current },
    );
    scene
      .init(host)
      .then(() => {
        if (cancelled) {
          scene.destroy();
          return;
        }
        sceneRef.current = scene;
        if (DEBUG) window.__durag = scene;
        scene.update(modelRef.current);
      })
      .catch((err: unknown) => {
        console.error('PixiJS failed to start', err);
      });
    return () => {
      cancelled = true;
      sceneRef.current = null;
      if (window.__durag === scene) delete window.__durag;
      scene.destroy(); // app.destroy(true): frees the WebGL context and textures
    };
  }, [generation]);

  useEffect(() => {
    sceneRef.current?.update(model);
  }, [model]);

  useEffect(() => {
    if (reject) sceneRef.current?.rejectCard(reject.cardId);
  }, [reject]);


  useEffect(() => {
    sceneRef.current?.setTopInset(topInset);
  }, [topInset]);

  // After a reconnect the server state is the only truth: drop optimistic moves.
  const prevConnection = useRef(connection);
  useEffect(() => {
    if (connection === 'open' && prevConnection.current !== 'open') sceneRef.current?.clearPending();
    prevConnection.current = connection;
  }, [connection]);

  return <div key={generation} ref={hostRef} className="pixi-host" />;
}
