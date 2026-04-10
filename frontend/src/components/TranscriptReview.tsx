import type { NoteLink } from '../api-types.gen'

interface TranscriptReviewProps {
  transcript: string
  noteLinks: NoteLink[]
}

export default function TranscriptReview({ transcript, noteLinks }: TranscriptReviewProps) {
  if (!transcript) return null

  return (
    <div className="transcript-review">
      <div className="transcript-review-layout">
        <div className="transcript-review-text">
          <h4 className="transcript-review-heading">Transcript</h4>
          <p className="transcript-review-body">{transcript}</p>
        </div>
        {noteLinks.length > 0 && (
          <div className="transcript-review-students">
            <h4 className="transcript-review-heading">Extracted Notes</h4>
            <ul className="transcript-review-list">
              {noteLinks.map((link) => (
                <li key={link.noteId} className="transcript-review-student">
                  <span className="transcript-review-student-name">{link.name}</span>
                  <span className="transcript-review-student-class">{link.className}</span>
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </div>
  )
}
