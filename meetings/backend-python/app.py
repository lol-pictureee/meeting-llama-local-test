"""
Meeting Summarizer — Whisper Transcription Service
Local faster-whisper HTTP endpoint for audio-to-text conversion.
No external APIs. No data leaves the machine.
"""

import base64
import logging
import os
import tempfile
import traceback
from contextlib import asynccontextmanager
from typing import List, Optional

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from faster_whisper import WhisperModel

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# --- Configuration from environment ---
WHISPER_MODEL_NAME = os.getenv("WHISPER_MODEL", "base")
CPU_MODE = os.getenv("WHISPER_CPU_MODE", "true").lower() == "true"
# Пустое значение = автоопределение языка
WHISPER_LANGUAGE = os.getenv("WHISPER_LANGUAGE", "").strip() or None
# int8 на CPU в разы быстрее float32 и почти не теряет точность
WHISPER_COMPUTE_TYPE = os.getenv(
    "WHISPER_COMPUTE_TYPE", "int8" if CPU_MODE else "float16"
)
WHISPER_BEAM_SIZE = int(os.getenv("WHISPER_BEAM_SIZE", "5"))
WHISPER_TMP_DIR = os.getenv("WHISPER_TMP_DIR", "/tmp/whisper")

ALLOWED_SUFFIXES = {".wav", ".mp3", ".m4a", ".mp4", ".webm", ".ogg", ".flac", ".aac", ".mkv", ".mov"}

# Global model instance (loaded once on startup)
model: Optional[WhisperModel] = None


class TranscribeRequest(BaseModel):
    """Expected request body from Go backend."""
    filename: str
    data: str  # base64-encoded audio bytes


class TranscriptSegment(BaseModel):
    start_time: float
    end_time: float
    text: str
    speaker: Optional[str] = None


class TranscribeResponse(BaseModel):
    segments: List[TranscriptSegment]
    language: Optional[str] = None
    duration: Optional[float] = None
    error: Optional[str] = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Load the Whisper model on startup (single load, cached)."""
    global model
    os.makedirs(WHISPER_TMP_DIR, exist_ok=True)
    logger.info(
        "Loading Whisper model '%s' (device=cpu, compute_type=%s, language=%s)",
        WHISPER_MODEL_NAME,
        WHISPER_COMPUTE_TYPE,
        WHISPER_LANGUAGE or "auto",
    )
    try:
        model = WhisperModel(
            WHISPER_MODEL_NAME,
            device="cpu",
            compute_type=WHISPER_COMPUTE_TYPE,
            cpu_threads=os.cpu_count() or 4,
            num_workers=1,
        )
        logger.info("Whisper model loaded successfully.")
    except Exception as e:
        logger.error("Failed to load Whisper model: %s", e)
        raise RuntimeError(f"Cannot load Whisper model: {e}") from e
    yield
    logger.info("Whisper service shutting down.")


app = FastAPI(title="Whisper Transcription Service", version="1.1.0", lifespan=lifespan)


@app.get("/health")
async def health():
    """Health check endpoint."""
    return {
        "status": "ok" if model is not None else "loading",
        "model": WHISPER_MODEL_NAME,
        "compute_type": WHISPER_COMPUTE_TYPE,
        "language": WHISPER_LANGUAGE or "auto",
    }


@app.post("/transcribe", response_model=TranscribeResponse)
async def transcribe(request: TranscribeRequest):
    """Transcribe uploaded audio using local faster-whisper."""
    if model is None:
        raise HTTPException(status_code=503, detail="Whisper model not loaded")

    try:
        audio_data = base64.b64decode(request.data)
    except Exception as e:
        raise HTTPException(status_code=400, detail=f"Invalid base64 data: {e}")

    if not audio_data:
        raise HTTPException(status_code=400, detail="Empty audio data")

    # Расширение важно: PyAV определяет контейнер по суффиксу файла.
    suffix = os.path.splitext(request.filename or "")[1].lower()
    if suffix not in ALLOWED_SUFFIXES:
        suffix = ".wav"

    tmp_path = None
    try:
        with tempfile.NamedTemporaryFile(
            dir=WHISPER_TMP_DIR, suffix=suffix, delete=False
        ) as tmp:
            tmp.write(audio_data)
            tmp_path = tmp.name

        logger.info(
            "Transcribing %.2f MB (%s) from %s",
            len(audio_data) / 1024 / 1024,
            suffix,
            request.filename,
        )

        segments, info = model.transcribe(
            tmp_path,
            language=WHISPER_LANGUAGE,
            beam_size=WHISPER_BEAM_SIZE,
            vad_filter=True,
            vad_parameters=dict(min_silence_duration_ms=500),
            condition_on_previous_text=False,
        )

        result_segments: List[TranscriptSegment] = []
        for seg in segments:
            text = (seg.text or "").strip()
            if text:
                result_segments.append(
                    TranscriptSegment(
                        start_time=float(seg.start),
                        end_time=float(seg.end),
                        text=text,
                    )
                )

        logger.info(
            "Transcription complete: %d segments, duration=%.1fs, language=%s (p=%.2f)",
            len(result_segments),
            getattr(info, "duration", 0.0) or 0.0,
            info.language,
            info.language_probability,
        )

        return TranscribeResponse(
            segments=result_segments,
            language=info.language,
            duration=float(getattr(info, "duration", 0.0) or 0.0),
        )

    except Exception as e:
        logger.error("Transcription failed: %s\n%s", e, traceback.format_exc())
        return TranscribeResponse(segments=[], error=f"Transcription failed: {e}")
    finally:
        if tmp_path and os.path.exists(tmp_path):
            try:
                os.remove(tmp_path)
            except OSError:
                logger.warning("Could not remove temp file %s", tmp_path)


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=int(os.getenv("WHISPER_PORT", "8081")))